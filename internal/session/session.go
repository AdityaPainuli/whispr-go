package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"whiper-go/internal/audio"
	"whiper-go/internal/engine"
	"whiper-go/internal/refine"
)

type State int

const (
	Idle State = iota
	Listening
	Flushing
)

// Controller is the dictation brain: it owns the state machine and the
// teardown ordering (mic stop → drain → flush → paste). Everything else
// (hotkey, tray) just calls Toggle and observes.
type Controller struct {
	eng   engine.Engine
	mic   audio.Capture
	paste func(string) error

	// OnPartial, if set, receives the growing hypothesis while listening.
	// Called from the feeder goroutine — keep it fast (UI update, print).
	OnPartial func(string)
	// OnError, if set, receives failures that sent us back to Idle.
	OnError func(error)
	// OnState, if set, fires on every state transition. Needed because
	// Flushing → Idle happens asynchronously after finish() — observers
	// can't see it by polling State() after Toggle returns.
	OnState func(State)

	// Refiner, if set, cleans the flushed text before pasting. It runs
	// under RefineTimeout; on timeout or error the RAW text is pasted.
	// The refiner can improve a dictation, never delay or lose one.
	Refiner       refine.Refiner
	RefineTimeout time.Duration

	mu     sync.Mutex
	state  State
	stream engine.Stream
	done   chan struct{} // closed by the feeder when the audio channel drains
	peak   int16         // loudest sample this dictation; written by feed, read after <-done
}

func New(eng engine.Engine, mic audio.Capture, paste func(string) error) *Controller {
	return &Controller{eng: eng, mic: mic, paste: paste}
}

func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Toggle is the only public action. Safe to call from any goroutine,
// including a jittery hotkey thread firing twice in a row.
func (c *Controller) Toggle() {
	c.mu.Lock()
	switch c.state {
	case Idle:
		err := c.startLocked()
		c.mu.Unlock()
		if err != nil {
			c.report(err)
		}
	case Listening:
		// Claim the Flushing state under the lock, then do the slow work
		// (drain + flush + paste) outside it. A second Toggle arriving
		// mid-flush hits the Flushing case below and bounces off.
		c.state = Flushing
		c.mu.Unlock()
		c.notify(Flushing)
		c.finish()
	case Flushing:
		c.mu.Unlock()
	}
}

// startLocked transitions Idle → Listening. Caller holds c.mu.
// Any failure cleans up and leaves us in Idle — never half-started.
func (c *Controller) startLocked() error {
	stream, err := c.eng.NewStream(nil)
	if err != nil {
		return fmt.Errorf("session: new stream: %w", err)
	}
	ch, err := c.mic.Start()
	if err != nil {
		stream.Close()
		return fmt.Errorf("session: mic start: %w", err)
	}

	c.stream = stream
	c.done = make(chan struct{})
	c.state = Listening

	go c.feed(stream, ch, c.done)
	c.notify(Listening)
	return nil
}

// feed is the conveyor-belt consumer: audio channel → engine. Exits when
// mic.Stop closes the channel, then signals done so finish can flush.
func (c *Controller) feed(stream engine.Stream, ch <-chan []int16, done chan struct{}) {
	defer close(done)
	last := ""
	for samples := range ch {
		for _, s := range samples {
			if s > c.peak {
				c.peak = s
			}
		}
		if err := stream.Feed(samples); err != nil {
			c.report(fmt.Errorf("session: feed: %w", err))
			continue
		}
		if c.OnPartial != nil {
			if p, err := stream.Partial(); err == nil && p != "" && p != last {
				c.OnPartial(p)
				last = p
			}
		}
	}
}

// finish runs the teardown sequence and returns to Idle no matter what —
// a stuck state machine is worse than a lost transcript.
func (c *Controller) finish() {
	c.mic.Stop() // no new parcels; closes the channel
	<-c.done     // feeder drained everything already on the belt

	text, err := c.stream.Flush()
	if err != nil {
		c.report(fmt.Errorf("session: flush: %w", err))
	} else if text != "" {
		if err := c.paste(c.refined(text)); err != nil {
			c.report(fmt.Errorf("session: paste: %w", err))
		}
	} else {
		// Empty transcript must never be silent — it's the signature of a
		// mic permission problem (macOS hands muted-permission apps pure
		// zeros instead of failing) or a muted/too-quiet input.
		if c.peak < 500 {
			c.report(fmt.Errorf("session: heard only silence (peak %d) — likely missing "+
				"microphone permission for this terminal, or input muted", c.peak))
		} else {
			c.report(fmt.Errorf("session: audio present (peak %d) but no words recognized", c.peak))
		}
	}
	c.peak = 0

	c.stream.Close()

	c.mu.Lock()
	c.stream = nil
	c.done = nil
	c.state = Idle
	c.mu.Unlock()
	c.notify(Idle)
}

func (c *Controller) report(err error) {
	if c.OnError != nil {
		c.OnError(err)
	}
}

func (c *Controller) notify(s State) {
	if c.OnState != nil {
		c.OnState(s)
	}
}

// refined runs the optional cleanup pass with a hard deadline. Fallback is
// always the raw transcript — a refine failure costs polish, not the text.
func (c *Controller) refined(text string) string {
	if c.Refiner == nil {
		return text
	}
	timeout := c.RefineTimeout
	if timeout <= 0 {
		// Output length tracks input length (editor contract), so the
		// budget scales with the dictation: ~430ms for a sentence,
		// capped at 2.5s for long rambles. Measured on M2: 30 words
		// ≈ 430ms, 60 words ≈ 1.2s.
		timeout = 600*time.Millisecond + time.Duration(len(text))*4*time.Millisecond
		if timeout > 2500*time.Millisecond {
			timeout = 2500 * time.Millisecond
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cleaned, err := c.Refiner.Refine(ctx, text)
	if err != nil {
		c.report(fmt.Errorf("session: refine (pasting raw): %w", err))
		return text
	}
	return cleaned
}
