package session

import (
	"fmt"
	"sync"

	"whiper-go/internal/audio"
	"whiper-go/internal/engine"
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

	mu     sync.Mutex
	state  State
	stream engine.Stream
	done   chan struct{} // closed by the feeder when the audio channel drains
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
	return nil
}

// feed is the conveyor-belt consumer: audio channel → engine. Exits when
// mic.Stop closes the channel, then signals done so finish can flush.
func (c *Controller) feed(stream engine.Stream, ch <-chan []int16, done chan struct{}) {
	defer close(done)
	last := ""
	for samples := range ch {
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
		if err := c.paste(text); err != nil {
			c.report(fmt.Errorf("session: paste: %w", err))
		}
	}

	c.stream.Close()

	c.mu.Lock()
	c.stream = nil
	c.done = nil
	c.state = Idle
	c.mu.Unlock()
}

func (c *Controller) report(err error) {
	if c.OnError != nil {
		c.OnError(err)
	}
}
