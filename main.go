package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"
	"os/signal"

	"github.com/mesilliac/pulse-simple"
	"gopkg.in/hraban/opus.v2"
)

const sampleRate = 48000
const channels = 1 // mono; 2 for stereo
const bufferSize = 1000
const frameSizeMs = 60 // msec

var l *log.Logger = log.New(os.Stderr, "", log.LstdFlags)

func main() {

	ss := pulse.SampleSpec{pulse.SAMPLE_S16LE, sampleRate, channels}
	ba := pulse.BufferAttr{Maxlength: 1000}
	stream1, err := pulse.NewStream("", "my app", pulse.STREAM_RECORD, "", "my stream", &ss, nil, &ba)
	if err != nil {
		l.Fatal(err)
	}
	defer stream1.Free()
	defer stream1.Drain()
	stream2, err := pulse.Playback("my app2", "my stream2", &ss)
	if err != nil {
		l.Fatal(err)
	}
	defer stream2.Free()
	defer stream2.Drain()

	quitChan := make(chan bool, 1)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		quitChan <- true
	}()

	var n int
	pcmBuf := make([]byte, ss.UsecToBytes(frameSizeMs*1000))
	opusBuf := make([]byte, bufferSize)
	frameSize := channels * frameSizeMs * sampleRate / 1000
	pcm2 := make([]int16, int(frameSize))

	for {
		select {
		case <-quitChan:
			l.Print("returning...")
			return
		default:
		}

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
