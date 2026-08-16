package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"whiper-go/internal/engine"
)

// --- fakes: prove the state machine without mic or model ---

type fakeStream struct {
	fed    int
	closed bool
}

func (s *fakeStream) Feed(pcm []int16) error    { s.fed++; return nil }
func (s *fakeStream) Partial() (string, error)  { return "partial", nil }
func (s *fakeStream) Flush() (string, error)    { return "hello world", nil }
func (s *fakeStream) Close() error              { s.closed = true; return nil }

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
