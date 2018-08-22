package main

import (
	"bytes"
	"encoding/binary"
	//"fmt"
	"log"
	"os"
	"time"

	"github.com/mesilliac/pulse-simple"
	"gopkg.in/hraban/opus.v2"
)

const sampleRate = 48000
const channels = 1 // mono; 2 for stereo
const bufferSize = 1000
const frameSizeMs = 60 // msec

var l *log.Logger = log.New(os.Stderr, "", log.LstdFlags)
var f *log.Logger = log.New(os.Stdout, "", log.Lmicroseconds)

//var f *log.Logger = log.New(new(bytes.Buffer), "", log.LstdFlags)

func main() {
	ss := pulse.SampleSpec{pulse.SAMPLE_S16LE, sampleRate, channels}
	//ba := pulse.BufferAttr{Maxlength: 100}
	//ba := pulse.BufferAttr{Maxlength: ^uint32(0) - 1}
	//stream, err := pulse.NewStream("", "my app", pulse.STREAM_RECORD, "", "my stream", &ss, nil, &ba)
	stream, err := pulse.Capture("my app", "my stream", &ss)
	if err != nil {
		l.Fatal(err)
	}
	defer stream.Free()
	defer stream.Drain()

	lat, _ := stream.Latency()
	f.Printf("latency %d %s\n", lat)

	var n int
	pcmBuf := make([]byte, ss.UsecToBytes(frameSizeMs*1000))

	f.Printf("pcmBuf len %d\n", len(pcmBuf))

	bufChan := make(chan []int16)
	go Encoder(bufChan)

	for {
		start1 := time.Now()
		n, err = stream.Read(pcmBuf)
		if err != nil {
			l.Fatalf("Couldn't read from pulse stream: %s\n", err)
		}
		end1 := time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("read wait time %s\n", end1.Sub(start1))
		}

		//f.Printf("pulse: read %d bytes, bytes %x\n", n, pcmBuf)

		pcm := make([]int16, n/2)

		buf := bytes.NewReader(pcmBuf)
		err := binary.Read(buf, binary.LittleEndian, &pcm)
		if err != nil {
			l.Fatal("binary.Read failed:", err)
		}

		select {
		case bufChan <- pcm:
		default:
			//		l.Printf("Encoder was not ready, missed %d samples\n", n)
		}
	}
}

func Encoder(in chan []int16) {

	bufChan := make(chan []byte)
	go Decoder(bufChan)

	opusBuf := make([]byte, bufferSize)
	var n int
	for {

		start1 := time.Now()
		pcm := <-in
		end1 := time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("encoder input wait time %s\n", end1.Sub(start1))
		}
		//f.Printf("pcm int16: %d\n", pcm)

		enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
		if err != nil {
			l.Fatalf("opus encoder error: %s\n", err)
		}
		start1 = time.Now()
		n, err = enc.Encode(pcm, opusBuf)
		if err != nil {
			l.Fatal("opus encode error: ", err)
		}
		end1 = time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("encode time %s\n", end1.Sub(start1))
		}

		select {
		case bufChan <- opusBuf[:n]:
		default:
			l.Printf("decoder was not ready, missed %d bytes\n", n)
		}
	}
}

func Decoder(in chan []byte) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		l.Fatalf("opus decoder error: %s\n", err)
	}

	pcmChan := make(chan []int16)
	go Writer(pcmChan)

	frameSize := channels * frameSizeMs * sampleRate / 1000
	pcm2 := make([]int16, int(frameSize))

	var n int
	for {
		start1 := time.Now()
		buf := <-in
		end1 := time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("decoder input wait time %s\n", end1.Sub(start1))
		}
		start1 = time.Now()
		n, err = dec.Decode(buf, pcm2)
		if err != nil {
			l.Fatal("opus decode error: ", err)
		}
		end1 = time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("decode time %s\n", end1.Sub(start1))
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
		start1 := time.Now()
		pcm := <-in
		end1 := time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("writer input wait time %s\n", end1.Sub(start1))
		}

		start1 = time.Now()
		err = binary.Write(stream, binary.LittleEndian, pcm)
		if err != nil {
			l.Fatal("binary.Write error: ", err)
		}
		end1 = time.Now()
		if end1.Sub(start1) > time.Duration(frameSizeMs*time.Millisecond) {
			f.Printf("pulse write wait time %s\n", end1.Sub(start1))
		}

		//f.Printf("%d samples, write time %s\n", len(pcm), end.Sub(start))
	}
}
