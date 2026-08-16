package main

import (
	"fmt"
	"os"

	"whiper-go/internal/audio"
	"whiper-go/internal/engine"
	"whiper-go/internal/hotkey"
	"whiper-go/internal/output"
	"whiper-go/internal/session"
	"whiper-go/internal/tray"
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

	mic, err := audio.New()
	if err != nil {
		panic(err)
	}

	ctl := session.New(eng, mic, output.Paste)
	ctl.OnError = func(err error) { fmt.Fprintln(os.Stderr, "error:", err) }
	ctl.OnState = tray.SetState

	// Hotkey loop moves off main: CGEventTaps run fine on any pinned thread
	// with a run loop, but the menu bar cannot — macOS wants UI on the main
	// thread, so systray gets it.
	fired := make(chan struct{}, 1)
	go func() {
		if err := hotkey.Run(fired); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()
	go func() {
		for range fired {
			ctl.Toggle()
		}
	}()

	// Blocks until Quit is chosen from the menu.
	tray.Run(
		func() { fmt.Println("ready — tap Option to start/stop dictation anywhere") },
		func() { // onExit: release the mic and the model, then leave
			mic.Close()
			eng.Close()
		},
	)
}
