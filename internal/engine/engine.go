package engine

import "context"

// config points at model files.
type Config struct {
	EncoderPath string
	DecoderPath string
	JoinerPath  string
	TokensPath  string
	NumThreads  int
	// Rule2Silence: seconds of post-speech silence that closes a segment
	// (endpoint detection). 0 means the 1.2s default.
	Rule2Silence float64
}

// Engine is created once at app start (model load ~2.2 s = prewarm)
type Engine interface {
	// NewStream behins one dcitation. Cheap (~ms), called on hotkey-down
	NewStream(ctx context.Context) (Stream, error)
	Close() error
}
