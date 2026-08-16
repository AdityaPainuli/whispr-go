package engine

import "context"

// config points at model files.
type Config struct {
	EncoderPath string
	DecoderPath string
	JoinerPath  string
	TokensPath  string
	NumThreads  int
}

// Engine is created once at app start (model load ~2.2 s = prewarm)
type Engine interface {
	// NewStream behins one dcitation. Cheap (~ms), called on hotkey-down
	NewStream(ctx context.Context) (Stream, error)
	Close() error
}
