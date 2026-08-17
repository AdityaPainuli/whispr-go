package session

import (
	"context"
	"fmt"
	"strings"
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
	// Corrections enables the cross-segment merge pass for spoken
	// self-corrections. Only set when the refiner model can handle them
	// (3B+); on smaller models a merge attempt can corrupt the text.
	Corrections bool

	mu     sync.Mutex
	state  State
	stream engine.Stream
	done   chan struct{} // closed by the feeder when the audio channel drains
	peak   int16         // loudest sample this dictation; written by feed, read after <-done

	// segments holds one entry per pause-closed segment, in speech order.
	// Refinement of segment N runs while the user speaks segment N+1, so
	// the wait at stop is one tail segment, not the whole dictation.
	// Raw text is kept for the cross-segment correction check in finish.
	// Written by the feeder, read by finish after <-done (like peak).
	segments []segment
}

type segment struct {
	raw     string
	refined <-chan string
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
	last, prefix := "", "" // prefix = raw text of segments already closed
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
		p, err := stream.Partial()
		if err != nil {
			continue
		}
		if c.OnPartial != nil && p != "" && prefix+p != last {
			c.OnPartial(prefix + p)
			last = prefix + p
		}
		// Endpoint = the model heard a pause. Close the segment: hand it
		// to the refiner NOW, while the user keeps talking, and reset the
		// stream so the next sentence decodes into a fresh hypothesis.
		if stream.IsEndpoint() {
			if strings.TrimSpace(p) != "" {
				c.segments = append(c.segments, segment{raw: p, refined: c.refineAsync(p)})
				prefix += p + " "
			}
			stream.Reset()
		}
	}
}

// refineAsync starts a refine in the background and returns its future.
// Buffered so the goroutine can finish even if the result is never read.
func (c *Controller) refineAsync(text string) <-chan string {
	ch := make(chan string, 1)
	go func() { ch <- c.refined(text) }()
	return ch
}

// finish runs the teardown sequence and returns to Idle no matter what —
// a stuck state machine is worse than a lost transcript.
func (c *Controller) finish() {
	c.mic.Stop() // no new parcels; closes the channel
	<-c.done     // feeder drained everything already on the belt

	tail, ferr := c.stream.Flush()
	if ferr != nil {
		c.report(fmt.Errorf("session: flush: %w", ferr))
		tail = ""
	}

	// Tail joins the queue like any other segment, then everything is
	// collected in speech order. Each future resolves within its own
	// refine timeout, so this wait is bounded; segments closed during
	// speech are almost always done already — the wait is just the tail.
	segs := c.segments
	if strings.TrimSpace(tail) != "" {
		segs = append(segs, segment{raw: tail, refined: c.refineAsync(tail)})
	}
	parts := make([]string, 0, len(segs))
	raws := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, <-s.refined)
		raws = append(raws, s.raw)
	}
	text := strings.Join(parts, " ")

	// A self-correction can straddle a pause ("meeting at 4 <pause> no
	// wait 5") — the per-segment refines can't see across the boundary.
	// The merge pass must run on the joined RAW text: a per-segment
	// refine strips an orphaned cue ("no wait, make it 5") without being
	// able to apply it, so the refined text looks innocent. Corrections
	// are rare, so most dictations skip this. On merge failure keep the
	// per-segment refined text — polish survives, the correction doesn't.
	if c.Corrections && len(segs) > 1 && c.Refiner != nil {
		if rawAll := strings.Join(raws, " "); refine.HasCorrectionCue(rawAll) {
			if merged, err := c.tryRefine(rawAll); err == nil {
				text = merged
			} else {
				c.report(fmt.Errorf("session: merge refine (keeping per-segment): %w", err))
			}
		}
	}

	if text != "" {
		if err := c.paste(text); err != nil {
			c.report(fmt.Errorf("session: paste: %w", err))
		}
	} else if ferr == nil {
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
	c.segments = nil

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
	cleaned, err := c.tryRefine(text)
	if err != nil {
		c.report(fmt.Errorf("session: refine (pasting raw): %w", err))
		return text
	}
	return cleaned
}

// tryRefine runs one refine under the deadline and surfaces the error so
// callers can pick their own fallback.
func (c *Controller) tryRefine(text string) (string, error) {
	timeout := c.RefineTimeout
	if timeout <= 0 {
		// Output length tracks input length (editor contract), so the
		// budget scales with the segment. Measured with Qwen2.5-3B on
		// Metal (M2): ~500ms for a sentence, ~1.9s for a 250-char forced
		// split — call it 8ms/char plus headroom. Segments are pause-
		// bounded so the cap only bites the rare merge pass over a long
		// dictation, where timing out just keeps the unmerged text.
		timeout = 700*time.Millisecond + time.Duration(len(text))*8*time.Millisecond
		if timeout > 3000*time.Millisecond {
			timeout = 3000 * time.Millisecond
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return c.Refiner.Refine(ctx, text)
}
