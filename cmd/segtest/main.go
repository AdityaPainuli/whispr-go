// segtest: end-to-end segmented-refine check without a mic. Streams a WAV
// (twice, with 1.5s silence between = forced endpoint) through the real
// session Controller, real engine, real refiner. Prints segments + timing.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/AdityaPainuli/whispr-go/internal/engine"
	"github.com/AdityaPainuli/whispr-go/internal/refine"
	"github.com/AdityaPainuli/whispr-go/internal/session"

	"golang.org/x/sys/unix"
)

type wavCapture struct {
	samples []int16
	ch      chan []int16
	fed     chan struct{}
}

func (c *wavCapture) Start() (<-chan []int16, error) {
	c.ch = make(chan []int16, 1024)
	const chunk = 1600 // 100ms
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for off := 0; off < len(c.samples); off += chunk {
			end := min(off+chunk, len(c.samples))
			c.ch <- c.samples[off:end]
			<-tick.C
		}
		close(c.fed)
	}()
	return c.ch, nil
}
func (c *wavCapture) Stop() error  { close(c.ch); return nil }
func (c *wavCapture) Close() error { return nil }

type logRefiner struct{ inner refine.Refiner }

func (l *logRefiner) Refine(ctx context.Context, text string) (string, error) {
	t := time.Now()
	out, err := l.inner.Refine(ctx, text)
	fmt.Printf("[refine start=%s dur=%v err=%v]\n  in : %s\n  out: %s\n",
		t.Format("15:04:05.000"), time.Since(t).Round(time.Millisecond), err, text, out)
	return out, err
}

func main() {
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

	// same RAM gate as cmd/app: 3B + corrections needs 16GB
	model, corrections := "models/qwen2.5-1.5b-instruct-q4_k_m.gguf", false
	if mem, err := unix.SysctlUint64("hw.memsize"); err == nil && mem >= 16<<30 {
		model, corrections = "models/qwen2.5-3b-instruct-q4_k_m.gguf", true
	}
	fmt.Printf("cleanup model: %s corrections=%v\n", model, corrections)
	llm := refine.NewLlamaServer("third_party/llama/llama-server", model, 8181)
	llm.Corrections = corrections
	if err := llm.Start(); err != nil {
		panic(err)
	}
	defer llm.Stop()

	// default: test WAV twice with 1.5s silence between (forces an endpoint).
	// with an argument: play that WAV once, as-is.
	path := "testdata/test_speech.wav"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	data := raw[44:]
	wav := make([]int16, len(data)/2)
	for i := range wav {
		wav[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	samples := wav
	if len(os.Args) == 1 {
		samples = append(append(append([]int16{}, wav...), make([]int16, 24000)...), wav...)
	}

	mic := &wavCapture{samples: samples, fed: make(chan struct{})}
	var stopped time.Time
	done := make(chan struct{})
	ctl := session.New(eng, mic, func(text string) error {
		fmt.Printf("\n[pasted in %v after stop]\n%s\n", time.Since(stopped), text)
		close(done)
		return nil
	})
	ctl.OnError = func(err error) { fmt.Fprintln(os.Stderr, "error:", err) }
	if os.Getenv("NOREFINE") == "" {
		ctl.Refiner = &logRefiner{llm}
		ctl.Corrections = corrections
	}

	feedDur := time.Duration(len(samples)) * time.Second / 16000
	fmt.Printf("feeding %v of audio at real time...\n", feedDur)
	ctl.Toggle() // start
	<-mic.fed    // all audio delivered at real-time pace; decode kept up
	stopped = time.Now()
	fmt.Printf("[stop at %s]\n", stopped.Format("15:04:05.000"))
	ctl.Toggle()
	<-done
}
