package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/AdityaPainuli/whispr-go/internal/audio"
	"github.com/AdityaPainuli/whispr-go/internal/engine"
	"github.com/AdityaPainuli/whispr-go/internal/hotkey"
	"github.com/AdityaPainuli/whispr-go/internal/models"
	"github.com/AdityaPainuli/whispr-go/internal/output"
	"github.com/AdityaPainuli/whispr-go/internal/paths"
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

// Set by bootstrap, released by onExit. The tray owns the main thread and
// the app's lifetime; everything else starts after the menu bar exists so
// download progress and errors have somewhere to show.
var (
	eng engine.Engine
	mic audio.Capture
	llm *refine.LlamaServer
)

func main() {
	// -debug prints every raw and refined transcript. Dev tool only —
	// default off, so normal runs never log what the user dictated.
	debug := flag.Bool("debug", false, "log raw/refined transcripts to stdout")
	flag.Parse()

	tray.Run(
		func() { go bootstrap(*debug) },
		func() { // onExit: release the mic and both models, then leave
			if mic != nil {
				mic.Close()
			}
			if eng != nil {
				eng.Close()
			}
			if llm != nil {
				llm.Stop()
			}
		},
	)
}

// fatal shows the error where a .app user can actually see it (menu bar),
// not just on a stderr nobody is watching.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	tray.SetStatus("⚠️ whispr: " + err.Error())
}

func bootstrap(debug bool) {
	// Corrections need the 3B model — 1.5B reversed corrections half the
	// time in testing, 3B went 4/4. But 3B resident on an 8GB machine
	// starves the ASR decoder (measured: a 24s dictation drained 2 minutes
	// late under memory pressure), so small machines get the 1.5B with the
	// cleanup-only prompt instead.
	corrections := false
	if mem, err := unix.SysctlUint64("hw.memsize"); err == nil && mem >= 16<<30 {
		corrections = true
	}

	// First run: fetch the models (~1.6GB) with progress in the menu bar.
	// Later runs: everything present, this returns instantly.
	dir := paths.ModelsDir()
	if err := models.Ensure(dir, corrections, tray.SetStatus); err != nil {
		fatal(err)
		return
	}

	tray.SetStatus("… loading")
	var err error
	eng, err = engine.NewSherpa(engine.Config{
		EncoderPath: dir + "/nemotron-en/encoder.int8.onnx",
		DecoderPath: dir + "/nemotron-en/decoder.int8.onnx",
		JoinerPath:  dir + "/nemotron-en/joiner.int8.onnx",
		TokensPath:  dir + "/nemotron-en/tokens.txt",
		NumThreads:  4,
	})
	if err != nil {
		fatal(err)
		return
	}

	mic, err = audio.New()
	if err != nil {
		fatal(err)
		return
	}

	ctl := session.New(eng, mic, output.Paste)
	ctl.OnError = func(err error) { fmt.Fprintln(os.Stderr, "error:", err) }
	ctl.OnState = tray.SetState

	// Cleanup LLM: optional by construction, started in the background —
	// the model load takes seconds and dictation doesn't need it to exist.
	// The refiner attaches via SetRefiner when healthy; anything dictated
	// before that pastes raw, exactly as before this feature existed.
	llm = refine.NewLlamaServer(paths.LlamaServer(), dir+"/"+models.CleanupModel(corrections), 8181)
	llm.Corrections = corrections
	go func() {
		if err := llm.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cleanup disabled:", err)
			return
		}
		var r refine.Refiner = llm
		if debug {
			// Log raw vs refined so ASR errors and cleanup errors can't be
			// confused: wrong words in RAW = the ASR misheard (mic, accent,
			// dropped audio); wrong words only in REFINED = cleanup broke it.
			r = &loggingRefiner{inner: llm}
		}
		ctl.SetRefiner(r, corrections)
		fmt.Println("cleanup model ready")
	}()

	// CGEventTaps run fine on any pinned thread with a run loop; the menu
	// bar already owns main, so the hotkey gets its own goroutine.
	fired := make(chan struct{}, 1)
	go func() {
		if err := hotkey.Run(fired); err != nil {
			fatal(err)
		}
	}()
	go func() {
		for range fired {
			ctl.Toggle()
		}
	}()

	tray.SetState(session.Idle)
	fmt.Println("ready — tap Option to start/stop dictation anywhere")
}
