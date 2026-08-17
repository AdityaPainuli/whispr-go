package engine

// Stream is one dictation session.
type Stream interface {
	// Feed accepts 16kHz mono s16 PCM. Called from the audio thread later,
	// so it must be fast and must not block.
	Feed(pcm []int16) error
	// Partial returns the current best hypothesis (live preview later).
	Partial() (string, error)
	// IsEndpoint reports whether the engine heard an utterance boundary
	// (trailing silence after speech). Check after Feed; pair with Reset.
	IsEndpoint() bool
	// Reset clears the hypothesis so the next Partial starts empty. Call
	// after capturing the segment text at an endpoint.
	Reset()
	// Flush signals end-of-speech, drains remaining decode, returns final text.
	Flush() (string, error)
	Close() error
}
