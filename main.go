package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/mesilliac/pulse-simple"
	"gopkg.in/hraban/opus.v2"
)

const sampleRate = 48000
const channels = 1 // mono; 2 for stereo
const bufferSize = 1000
const frameSizeMs = 60 // 2.5, 5, 10, 20, 40, 60
const frameSize = channels * frameSizeMs * sampleRate / 1000

var l *log.Logger = log.New(os.Stderr, "", log.LstdFlags)
var wg sync.WaitGroup

func main() {

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

	wg.Add(1)
	go EncDec(dataChan, quitChan)

main:
	for {
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

		select {
		case <-quitChan:
			break main
		case dataChan <- pcm:
		default:
			fmt.Printf("EncDec not ready, dropped %d samples\n", len(pcm))
		}
	}

	wg.Wait()
}

func EncDec(dataChan chan []int16, quitChan chan struct{}) {
	wg.Done()
	ss := pulse.SampleSpec{pulse.SAMPLE_S16LE, sampleRate, channels}
	ba2 := pulse.NewBufferAttr()
	ba2.Tlength = uint32(ss.UsecToBytes(frameSizeMs * 1000))
	stream2, err := pulse.NewStream("", "my app2", pulse.STREAM_PLAYBACK, "", "my stream2", &ss, nil, ba2)
	if err != nil {
		l.Fatal(err)
	}
	lat2, _ := stream2.Latency()
	fmt.Printf("playback latency %s\n", time.Duration(lat2*1000))
	defer stream2.Free()
	defer stream2.Drain()

	var n int
	var pcm []int16
	pcm2 := make([]int16, int(frameSize))
	opusBuf := make([]byte, bufferSize)

	for {
		select {
		case <-quitChan:
			return
		case pcm = <-dataChan:
		}

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
		if err != nil {
			l.Fatal("opus encoder error: ", err)
		}

		n, err = enc.Encode(pcm, opusBuf)
		if err != nil {
			l.Fatal("opus encode error: ", err)
		}

		dec, err := opus.NewDecoder(sampleRate, channels)
		if err != nil {
			l.Fatal("opus decoder error: ", err)
		}

		n, err = dec.Decode(opusBuf[:n], pcm2)
		if err != nil {
			l.Fatal("opus decode error: ", err)
		}

		err = binary.Write(stream2, binary.LittleEndian, pcm2)
		if err != nil {
			l.Fatal("binary.Write error: ", err)
		}
	}
}
