package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AdityaPainuli/whispr-go/internal/engine"
)

// --- fakes: prove the state machine without mic or model ---

type fakeStream struct {
	fed    int
	closed bool
}

func (s *fakeStream) Feed(pcm []int16) error   { s.fed++; return nil }
func (s *fakeStream) Partial() (string, error) { return "partial", nil }
func (s *fakeStream) IsEndpoint() bool         { return false }
func (s *fakeStream) Reset()                   {}
func (s *fakeStream) Flush() (string, error)   { return "hello world", nil }
func (s *fakeStream) Close() error             { s.closed = true; return nil }

type fakeEngine struct {
	stream *fakeStream
	err    error
}

func (e *fakeEngine) NewStream(_ context.Context) (engine.Stream, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.stream = &fakeStream{}
	return e.stream, nil
}
func (e *fakeEngine) Close() error { return nil }

type fakeCapture struct {
	ch  chan []int16
	err error
}

func (c *fakeCapture) Start() (<-chan []int16, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.ch = make(chan []int16, 4)
	return c.ch, nil
}
func (c *fakeCapture) Stop() error {
	if c.ch != nil {
		close(c.ch)
		c.ch = nil
	}
	return nil
}
func (c *fakeCapture) Close() error { return c.Stop() }

// --- tests ---

func TestToggleStartStop(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })

	ctl.Toggle()
	if ctl.State() != Listening {
		t.Fatalf("after first toggle: state = %v, want Listening", ctl.State())
	}

	mic.ch <- make([]int16, 160) // one parcel of audio flows through

	ctl.Toggle()
	if ctl.State() != Idle {
		t.Fatalf("after second toggle: state = %v, want Idle", ctl.State())
	}
	if pasted != "hello world" {
		t.Fatalf("pasted = %q, want %q", pasted, "hello world")
	}
	if !eng.stream.closed {
		t.Fatal("stream not closed after finish")
	}
}

func TestToggleTwiceRestarts(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{}
	ctl := New(eng, mic, func(string) error { return nil })

	for i := 0; i < 2; i++ { // start → stop, twice: no deadlock, no double-start
		ctl.Toggle()
		if ctl.State() != Listening {
			t.Fatalf("cycle %d: not Listening after start", i)
		}
		ctl.Toggle()
		if ctl.State() != Idle {
			t.Fatalf("cycle %d: not Idle after stop", i)
		}
	}
}

func TestMicFailureReturnsToIdle(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{err: errors.New("no mic")}
	var gotErr error
	ctl := New(eng, mic, func(string) error { return nil })
	ctl.OnError = func(err error) { gotErr = err }

	ctl.Toggle()
	if ctl.State() != Idle {
		t.Fatalf("state = %v, want Idle after mic failure", ctl.State())
	}
	if gotErr == nil {
		t.Fatal("OnError not called")
	}
	if eng.stream == nil || !eng.stream.closed {
		t.Fatal("stream leaked: not closed after mic failure")
	}
}

type fakeRefiner struct {
	delay time.Duration
	out   string
	err   error
}

func (r *fakeRefiner) Refine(ctx context.Context, text string) (string, error) {
	select {
	case <-time.After(r.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if r.err != nil {
		return "", r.err
	}
	return r.out, nil
}

func TestRefinerCleansText(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })
	ctl.Refiner = &fakeRefiner{out: "Hello, world."}

	ctl.Toggle()
	ctl.Toggle()
	if pasted != "Hello, world." {
		t.Fatalf("pasted = %q, want refined text", pasted)
	}
}

func TestSlowRefinerFallsBackToRaw(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })
	ctl.Refiner = &fakeRefiner{delay: time.Second, out: "too late"}
	ctl.RefineTimeout = 20 * time.Millisecond

	var refineErr error
	ctl.OnError = func(err error) { refineErr = err }

	ctl.Toggle()
	ctl.Toggle()
	if pasted != "hello world" { // the fake stream's raw flush text
		t.Fatalf("pasted = %q, want raw fallback", pasted)
	}
	if refineErr == nil {
		t.Fatal("timeout should have been reported via OnError")
	}
}

func TestFailingRefinerFallsBackToRaw(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })
	ctl.Refiner = &fakeRefiner{err: errors.New("model exploded")}

	ctl.Toggle()
	ctl.Toggle()
	if pasted != "hello world" {
		t.Fatalf("pasted = %q, want raw fallback", pasted)
	}
}

// segStream simulates "first sentence" <pause> "second sentence": the
// endpoint fires after the first parcel, Reset clears the hypothesis, and
// the second sentence is still in flight at Flush time (the tail).
type segStream struct {
	fed    int
	cur    string
	resets int
	closed bool
}

func (s *segStream) Feed(pcm []int16) error {
	s.fed++
	if s.fed == 1 {
		s.cur = "first sentence"
	} else {
		s.cur = "second sentence"
	}
	return nil
}
func (s *segStream) Partial() (string, error) { return s.cur, nil }
func (s *segStream) IsEndpoint() bool         { return s.fed == 1 && s.cur != "" }
func (s *segStream) Reset()                   { s.resets++; s.cur = "" }
func (s *segStream) Flush() (string, error)   { return s.cur, nil }
func (s *segStream) Close() error             { s.closed = true; return nil }

type segEngine struct{ stream *segStream }

func (e *segEngine) NewStream(_ context.Context) (engine.Stream, error) {
	e.stream = &segStream{}
	return e.stream, nil
}
func (e *segEngine) Close() error { return nil }

// markRefiner tags each refine call so the test can see per-segment calls.
type markRefiner struct {
	mu    sync.Mutex
	calls []string
}

func (r *markRefiner) Refine(_ context.Context, text string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, text)
	r.mu.Unlock()
	return "<" + text + ">", nil
}

func TestSegmentsRefinedIndependentlyAndJoined(t *testing.T) {
	eng := &segEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })
	ref := &markRefiner{}
	ctl.Refiner = ref

	ctl.Toggle()
	mic.ch <- make([]int16, 160) // sentence 1, endpoint fires
	mic.ch <- make([]int16, 160) // sentence 2, still open at stop
	ctl.Toggle()

	if want := "<first sentence> <second sentence>"; pasted != want {
		t.Fatalf("pasted = %q, want %q", pasted, want)
	}
	if len(ref.calls) != 2 {
		t.Fatalf("refine calls = %v, want one per segment", ref.calls)
	}
	if eng.stream.resets != 1 {
		t.Fatalf("stream resets = %d, want 1 (after the endpoint)", eng.stream.resets)
	}
}

// corrStream simulates a correction across a pause:
// "meeting at four pm" <pause> "no wait make it five pm".
type corrStream struct {
	fed    int
	cur    string
	closed bool
}

func (s *corrStream) Feed(pcm []int16) error {
	s.fed++
	if s.fed == 1 {
		s.cur = "meeting at four pm"
	} else {
		s.cur = "no wait make it five pm"
	}
	return nil
}
func (s *corrStream) Partial() (string, error) { return s.cur, nil }
func (s *corrStream) IsEndpoint() bool         { return s.fed == 1 && s.cur != "" }
func (s *corrStream) Reset()                   { s.cur = "" }
func (s *corrStream) Flush() (string, error)   { return s.cur, nil }
func (s *corrStream) Close() error             { s.closed = true; return nil }

type corrEngine struct{ stream *corrStream }

func (e *corrEngine) NewStream(_ context.Context) (engine.Stream, error) {
	e.stream = &corrStream{}
	return e.stream, nil
}
func (e *corrEngine) Close() error { return nil }

func TestCorrectionCueTriggersMergePass(t *testing.T) {
	eng := &corrEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })
	ref := &markRefiner{}
	ctl.Refiner = ref
	ctl.Corrections = true

	ctl.Toggle()
	mic.ch <- make([]int16, 160)
	mic.ch <- make([]int16, 160)
	ctl.Toggle()

	// Two per-segment refines plus one merge pass, because the raw text
	// contains the "no wait" cue. The merge input must be the joined RAW
	// text — the per-segment pass strips orphaned cues, so the refined
	// text no longer carries the correction signal.
	if len(ref.calls) != 3 {
		t.Fatalf("refine calls = %v, want 2 segments + 1 merge", ref.calls)
	}
	if want := "meeting at four pm no wait make it five pm"; ref.calls[2] != want {
		t.Fatalf("merge input = %q, want joined raw text %q", ref.calls[2], want)
	}
	if want := "<meeting at four pm no wait make it five pm>"; pasted != want {
		t.Fatalf("pasted = %q, want merge output %q", pasted, want)
	}
}

func TestNoCueSkipsMergePass(t *testing.T) {
	eng := &segEngine{} // "first sentence" / "second sentence" — no cue
	mic := &fakeCapture{}
	ctl := New(eng, mic, func(string) error { return nil })
	ref := &markRefiner{}
	ctl.Refiner = ref
	ctl.Corrections = true

	ctl.Toggle()
	mic.ch <- make([]int16, 160)
	mic.ch <- make([]int16, 160)
	ctl.Toggle()

	if len(ref.calls) != 2 {
		t.Fatalf("refine calls = %v, want exactly one per segment, no merge", ref.calls)
	}
}

func TestCorrectionsOffSkipsMergePass(t *testing.T) {
	eng := &corrEngine{} // raw text carries a "no wait" cue
	mic := &fakeCapture{}
	ctl := New(eng, mic, func(string) error { return nil })
	ref := &markRefiner{}
	ctl.Refiner = ref // Corrections deliberately left false (small model)

	ctl.Toggle()
	mic.ch <- make([]int16, 160)
	mic.ch <- make([]int16, 160)
	ctl.Toggle()

	if len(ref.calls) != 2 {
		t.Fatalf("refine calls = %v, want per-segment only — merge needs Corrections", ref.calls)
	}
}

func TestSegmentRefineFailureFallsBackPerSegment(t *testing.T) {
	eng := &segEngine{}
	mic := &fakeCapture{}
	var pasted string
	ctl := New(eng, mic, func(s string) error { pasted = s; return nil })
	ctl.Refiner = &fakeRefiner{err: errors.New("model exploded")}

	ctl.Toggle()
	mic.ch <- make([]int16, 160)
	mic.ch <- make([]int16, 160)
	ctl.Toggle()

	// Both segments fall back to their raw text; nothing is lost.
	if want := "first sentence second sentence"; pasted != want {
		t.Fatalf("pasted = %q, want raw fallback %q", pasted, want)
	}
}

func TestPartialCallback(t *testing.T) {
	eng := &fakeEngine{}
	mic := &fakeCapture{}
	ctl := New(eng, mic, func(string) error { return nil })

	got := make(chan string, 1)
	ctl.OnPartial = func(p string) {
		select {
		case got <- p:
		default:
		}
	}

	ctl.Toggle()
	mic.ch <- make([]int16, 160)

	select {
	case p := <-got:
		if p != "partial" {
			t.Fatalf("partial = %q", p)
		}
	case <-time.After(time.Second):
		t.Fatal("OnPartial never called")
	}
	ctl.Toggle()
}
