package main

import (
	"fmt"
	"log"

	"github.com/mesilliac/pulse-simple"
	"gopkg.in/hraban/opus.v2"
)

const sampleRate = 48000
const channels = 1 // mono; 2 for stereo
const bufferSize = 1000
const opusFrameSize = 20 // msec

func main() {

	ss := pulse.SampleSpec{pulse.SAMPLE_S16LE, sampleRate, channels}
	stream, err := pulse.Capture("my app", "my stream", &ss)
	if err != nil {
		log.Fatal(err)
	}

	var n, n2 int
	pcmBuf := make([]byte, ss.UsecToBytes(opusFrameSize*1000))
	opusBuf := make([]byte, bufferSize)
	for {
		n, err = stream.Read(pcmBuf)
		if err != nil {
			log.Fatalf("Couldn't read from pulse stream: %s\n", err)
		}

		fmt.Printf("pulse: read %d bytes, bytes %x\n", n, pcmBuf)

		// https://github.com/hraban/opus/blob/v2/stream_test.go#L81
		numSamples := n / 2
		pcm := make([]int16, numSamples)
		for i := 0; i < numSamples; i++ {
			pcm[i] += int16(pcmBuf[i*2])
			pcm[i] += int16(pcmBuf[i*2+1]) << 8
		}

		fmt.Printf("pcm int16: %d\n", pcm)

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
		if err != nil {
			log.Fatalf("opus encoder error: %s\n", err)
		}
		n2, err = enc.Encode(pcm, opusBuf)
		if err != nil {
			log.Fatal("opus encode error: ", err)
		}
		opusBuf = opusBuf[:n2] // only the first N bytes are opus data. Just like io.Reader.

		fmt.Printf("opus enc: read %d bytes\n", n2)

	}

}
