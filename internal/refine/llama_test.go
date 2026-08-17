package refine

import "testing"

func TestHasCorrectionCue(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"let's do the meeting at 4 pm or wait let's make it 5 pm", true},
		{"send it to sarah no wait send it to mike", true},
		{"tell John to push the release scratch that hold it", true},
		{"the budget is ten thousand I mean fifteen", true},
		{"we need three servers make that five", true},
		{"Or Wait, Let's Make It 5 PM.", true}, // case-insensitive, post-refine text
		{"so the demo went really well they want a follow up", false},
		{"we waited for the results and they were good", false},
		{"no update from the vendor yet", false},
		{"", false},
	}
	for _, c := range cases {
		if got := HasCorrectionCue(c.text); got != c.want {
			t.Errorf("HasCorrectionCue(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
