package refine

import "context"

// Refiner cleans up raw dictated text: punctuation, capitalization,
// disfluencies, self-corrections. Implementations must respect ctx —
// the session runs refinement under a hard timeout and pastes the raw
// text if it expires. A Refiner can improve the text, never delay it.
type Refiner interface {
	Refine(ctx context.Context, text string) (string, error)
}
