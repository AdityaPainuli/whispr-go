package main

import (
	"context"
	"fmt"
	"os"

	"github.com/AdityaPainuli/whispr-go/internal/audio"
	"github.com/AdityaPainuli/whispr-go/internal/engine"
	"github.com/AdityaPainuli/whispr-go/internal/hotkey"
	"github.com/AdityaPainuli/whispr-go/internal/output"
	"github.com/AdityaPainuli/whispr-go/internal/refine"
	"github.com/AdityaPainuli/whispr-go/internal/session"
	"github.com/AdityaPainuli/whispr-go/internal/tray"

	"golang.org/x/sys/unix"
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
	//
	// Model is RAM-gated. Self-correction ("no wait, make it 5pm") needs
	// the 3B — 1.5B reversed corrections half the time, 3B went 4/4. But
	// 3B resident on an 8GB machine starves the ASR decoder (measured: a
	// 24s dictation drained 2 minutes late under the memory pressure), so
	// small machines get the 1.5B with the cleanup-only prompt instead.
	model, corrections := "models/qwen2.5-1.5b-instruct-q4_k_m.gguf", false
	if mem, err := unix.SysctlUint64("hw.memsize"); err == nil && mem >= 16<<30 {
		model, corrections = "models/qwen2.5-3b-instruct-q4_k_m.gguf", true
	}
	llm := refine.NewLlamaServer("third_party/llama/llama-server", model, 8181)
	llm.Corrections = corrections
	fmt.Printf("starting cleanup model (%s, corrections=%v)....\n", model, corrections)
	if err := llm.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "cleanup disabled:", err)
	} else {
		// Log raw vs refined so ASR errors and cleanup errors can't be
		// confused: wrong words in RAW = the ASR misheard (mic, accent,
		// dropped audio); wrong words only in REFINED = cleanup broke it.
		ctl.Refiner = &loggingRefiner{inner: llm}
		ctl.Corrections = corrections
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
