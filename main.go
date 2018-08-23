package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/mesilliac/pulse-simple"
	"github.com/pions/webrtc"
	"github.com/pions/webrtc/pkg/ice"
	"gopkg.in/hraban/opus.v2"
)

const sampleRate = 48000
const channels = 1 // mono; 2 for stereo
const bufferSize = 1000
const frameSizeMs = 60 // 2.5, 5, 10, 20, 40, 60

var l *log.Logger = log.New(os.Stderr, "", log.LstdFlags)
var wg sync.WaitGroup

type Stats struct {
	sync.Mutex
	TotalPcm  uint64
	TotalOpus uint64
	Last      time.Time
}

var stats Stats = Stats{}

func main() {

	reader := bufio.NewReader(os.Stdin)
	rawSd, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		l.Fatal(err)
	}

	// enable echo-cancellation (this didn't seem to make any difference for me!?)
	err = os.Setenv("PULSE_PROP", "filter.want=echo-cancel")
	if err != nil {
		l.Fatal(err)
	}

	ss := pulse.SampleSpec{pulse.SAMPLE_S16LE, sampleRate, channels}
	// request desired latency as per:
	// https://www.freedesktop.org/wiki/Software/PulseAudio/Documentation/Developer/Clients/LatencyControl/
	ba := pulse.NewBufferAttr()
	ba.Fragsize = uint32(ss.UsecToBytes(frameSizeMs * 1000))
	stream1, err := pulse.NewStream("", "my app", pulse.STREAM_RECORD, "", "my stream", &ss, nil, ba)
	if err != nil {
		l.Fatal(err)
	}

	lat1, _ := stream1.Latency()
	fmt.Printf("record latency %s\n", time.Duration(lat1*1000))
	defer stream1.Free()
	defer stream1.Drain()

	quitChan := make(chan struct{})
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Println("Interrupted, quitting...")
		close(quitChan)
	}()

	var n int
	pcmBuf := make([]byte, ss.UsecToBytes(frameSizeMs*1000))

	dataChan := make(chan []int16)
	rtcChan := make(chan webrtc.RTCSample)

	wg.Add(2)
	go Encode(dataChan, rtcChan, quitChan)
	go WebRTCPipe(rtcChan, quitChan, rawSd)

main:
	for {
		// read from the Pulseaudio record stream
		n, err = stream1.Read(pcmBuf)
		if err != nil {
			l.Fatal("Couldn't read from pulse stream: ", err)
		}

		pcm := make([]int16, n/2)
		buf := bytes.NewReader(pcmBuf[:n])
		err := binary.Read(buf, binary.LittleEndian, &pcm)
		if err != nil {
			l.Fatal("binary.Read failed:", err)
		}

		stats.Lock()
		stats.TotalPcm += uint64(n)
		stats.Unlock()

		select {
		case <-quitChan:
			break main
		case dataChan <- pcm:
		default:
			fmt.Printf("Encode not ready, dropped %d samples\n", len(pcm))
		}
	}

	wg.Wait()
}

func Encode(inChan chan []int16, outChan chan webrtc.RTCSample, quitChan chan struct{}) {
	defer wg.Done()

	var n int
	var pcm []int16
	frameSize := channels * frameSizeMs * sampleRate / 1000
	opusBuf := make([]byte, bufferSize)

	for {
		select {
		case <-quitChan:
			fmt.Println("quiting Opus Encode")
			return
		case pcm = <-inChan:
		}

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
		if err != nil {
			l.Fatal("opus encoder error: ", err)
		}

		// encode PCM to Opus
		n, err = enc.Encode(pcm, opusBuf)
		if err != nil {
			l.Fatal("opus encode error: ", err)
		}
		stats.Lock()
		stats.TotalOpus += uint64(n)
		stats.Unlock()

		rtcSample := webrtc.RTCSample{Data: opusBuf, Samples: uint32(frameSize)}
		select {
		case outChan <- rtcSample:
		default:
			fmt.Printf("WebRTC not ready, dropped %d samples\n", frameSize)
		}

		/*
			if time.Now().Sub(stats.Last) > 2*time.Second {
				fmt.Printf("total pcm %d, total opus %d, %.2f%% data saved\n", stats.TotalPcm, stats.TotalOpus, 100-float64(stats.TotalOpus)/float64(stats.TotalPcm)*100)
				stats.Last = time.Now()
			}
			stats.Unlock()
		*/
	}
}

func WebRTCPipe(inChan chan webrtc.RTCSample, quitChan chan struct{}, rawSd string) {
	defer wg.Done()

	fmt.Println("")
	sd, err := base64.StdEncoding.DecodeString(rawSd)
	if err != nil {
		panic(err)
	}

	/* Everything below is the pion-WebRTC API, thanks for using it! */

	// Setup the codecs you want to use.
	// We'll use the default ones but you can also define your own
	webrtc.RegisterDefaultCodecs()

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.New(webrtc.RTCConfiguration{
		ICEServers: []webrtc.RTCICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange = func(connectionState ice.ConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
	}

	// Create a audio track
	opusTrack, err := peerConnection.NewRTCTrack(webrtc.DefaultPayloadTypeOpus, "audio", "pion1")
	if err != nil {
		panic(err)
	}
	_, err = peerConnection.AddTrack(opusTrack)
	if err != nil {
		panic(err)
	}

	// Set the remote SessionDescription
	offer := webrtc.RTCSessionDescription{
		Type: webrtc.RTCSdpTypeOffer,
		Sdp:  string(sd),
	}
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}

	// Sets the LocalDescription, and starts our UDP listeners
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// Get the LocalDescription and take it to base64 so we can paste in browser
	fmt.Println(base64.StdEncoding.EncodeToString([]byte(answer.Sdp)))

	for {
		select {
		case <-quitChan:
			fmt.Println("quiting WebRTC pipe")
			return
		case s := <-inChan:
			opusTrack.Samples <- s
		}
	}
}
