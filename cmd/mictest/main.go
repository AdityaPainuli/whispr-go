package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"whiper-go/internal/audio"
	"whiper-go/internal/engine"
	"whiper-go/internal/session"
)

func main() {
	fmt.Println("loading model....")
	eng, err := engine.NewSherpa(engine.Config{
		EncoderPath: "models/nemotron-en/encoder.int8.onnx",
		DecoderPath: "models/nemotron-en/decoder.int8.onnx",
		JoinerPath:  "models/nemotron-en/joiner.int8.onnx",
		TokensPath:  "models/nemotron-en/tokens.txt",
		NumThreads:  4,
	})
	if err != nil {
		panic(err)
	}
	defer eng.Close()

	mic, err := audio.New()
	if err != nil {
		panic(err)
	}
	defer mic.Close()

	// Fake paste until milestone 4; also times the stop-to-paste path.
	var stopped time.Time
	ctl := session.New(eng, mic, func(text string) error {
		fmt.Printf("\n[pasted in %v] %s\n", time.Since(stopped), text)
		return nil
	})
	ctl.OnPartial = func(p string) { fmt.Printf("\r\033[K[partial] %s", p) }
	ctl.OnError = func(err error) { fmt.Fprintln(os.Stderr, "\nerror:", err) }

	fmt.Println("Enter = start/stop dictation, Ctrl+C = quit")
	in := bufio.NewReader(os.Stdin)
	for {
		in.ReadString('\n')
		if ctl.State() == session.Listening {
			stopped = time.Now()
		}
		ctl.Toggle()
		switch ctl.State() {
		case session.Listening:
			fmt.Println("listening...")
		case session.Idle:
			fmt.Println("idle")
		}
	}
}
