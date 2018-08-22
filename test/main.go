package main

import (
	"bytes"
	"encoding/binary"
	//"fmt"
	"log"
	"os"

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
	stream, err := pulse.Capture("my app", "my stream", &ss)
	if err != nil {
		l.Fatal(err)
	}

	var n, n2 int
	pcmBuf := make([]byte, ss.UsecToBytes(frameSizeMs*1000))
	opusBuf := make([]byte, bufferSize)

	bufChan := make(chan []byte)
	go Decoder(bufChan)

	for {
		n, err = stream.Read(pcmBuf)
		if err != nil {
			l.Fatalf("Couldn't read from pulse stream: %s\n", err)
		}

		//fmt.Printf("pulse: read %d bytes, bytes %x\n", n, pcmBuf)

		pcm := make([]int16, n/2)

		buf := bytes.NewReader(pcmBuf)
		err := binary.Read(buf, binary.LittleEndian, &pcm)
		if err != nil {
			l.Fatal("binary.Read failed:", err)
		}

		//fmt.Printf("pcm int16: %d\n", pcm)

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
		if err != nil {
			l.Fatalf("opus encoder error: %s\n", err)
		}
		n2, err = enc.Encode(pcm, opusBuf)
		if err != nil {
			l.Fatal("opus encode error: ", err)
		}

		select {
		case bufChan <- opusBuf[:n2]:
		default:
			l.Printf("decoder was not ready, missed %d bytes\n", n2)
		}
	}
}

func Decoder(in chan []byte) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		l.Fatalf("opus decoder error: %s\n", err)
	}

	pcmChan := make(chan []int16, 100)
	go Writer(pcmChan)

	frameSize := channels * frameSizeMs * sampleRate / 1000
	pcm2 := make([]int16, int(frameSize))

	var n int
	for {
		buf := <-in
		n, err = dec.Decode(buf, pcm2)
		if err != nil {
			l.Fatal("opus decode error: ", err)
		}

		select {
		case pcmChan <- pcm2[:n]:
		default:
			l.Printf("writer was not ready, missed %d samples\n", n)
		}

	}

}

func Writer(in chan []int16) {

	ss := pulse.SampleSpec{pulse.SAMPLE_S16LE, sampleRate, channels}
	stream, err := pulse.Playback("my app2", "my stream2", &ss)
	if err != nil {
		l.Fatal(err)
	}
	defer stream.Free()
	defer stream.Drain()
	//buf := new(bytes.Buffer)
	for {
		pcm := <-in

		err = binary.Write(stream, binary.LittleEndian, pcm)
		if err != nil {
			l.Fatal("binary.Write error: ", err)
		}
	}
}
