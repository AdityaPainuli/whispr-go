package main

import (
	"context"
	"fmt"
	"os"

	"whiper-go/internal/audio"
	"whiper-go/internal/engine"
	"whiper-go/internal/hotkey"
	"whiper-go/internal/output"
	"whiper-go/internal/refine"
	"whiper-go/internal/session"
	"whiper-go/internal/tray"
)

// loggingRefiner prints raw and refined text so failures can be located.
type loggingRefiner struct {
	inner refine.Refiner
}

func (l *loggingRefiner) Refine(ctx context.Context, text string) (string, error) {
	fmt.Printf("[raw]     %s\n", text)
	out, err := l.inner.Refine(ctx, text)
	if err != nil {
		fmt.Printf("[refined] FAILED (%v) — pasting raw\n", err)
		return out, err
	}
	fmt.Printf("[refined] %s\n", out)
	return out, nil
}

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

	// Cleanup LLM: optional by construction. Missing binary or model →
	// warn and dictate raw, exactly as before this feature existed.
	llm := refine.NewLlamaServer(
		"third_party/llama/llama-server",
		"models/qwen2.5-1.5b-instruct-q4_k_m.gguf",
		8181,
	)
	fmt.Println("starting cleanup model....")
	if err := llm.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "cleanup disabled:", err)
	} else {
		// Log raw vs refined so ASR errors and cleanup errors can't be
		// confused: wrong words in RAW = the ASR misheard (mic, accent,
		// dropped audio); wrong words only in REFINED = cleanup broke it.
		ctl.Refiner = &loggingRefiner{inner: llm}
		defer llm.Stop()
	}

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
		func() { // onExit: release the mic and both models, then leave
			mic.Close()
			eng.Close()
			llm.Stop()
		},
	)
}
