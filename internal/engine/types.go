package engine

// Stream is one dictation session.
type Stream interface {
	// Feed accepts 16kHz mono s16 PCM. Called from the audio thread later,
	// so it must be fast and must not block.
	Feed(pcm []int16) error
	// Partial returns the current best hypothesis (live preview later).
	Partial() (string, error)
	// Flush signals end-of-speech, drains remaining decode, returns final text.
	Flush() (string, error)
	Close() error
}
