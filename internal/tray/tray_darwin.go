package tray

import (
	"fyne.io/systray"

	"github.com/AdityaPainuli/whispr-go/internal/session"
)

// Run owns the menu bar item. Must be called from the main goroutine and
// blocks until Quit is chosen (macOS rule: UI lives on the main thread).
// onReady fires once the item exists — wire callbacks there. onExit fires
// after Quit for cleanup.
func Run(onReady func(), onExit func()) {
	systray.Run(func() {
		systray.SetTitle(iconFor(session.Idle))
		systray.SetTooltip("whispr — local dictation")

		quit := systray.AddMenuItem("Quit whispr", "Stop dictation and exit")
		go func() {
			<-quit.ClickedCh
			systray.Quit()
		}()

		onReady()
	}, onExit)
}

// SetState updates the menu bar glyph. Safe to call from any goroutine —
// systray marshals to the UI thread internally.
func SetState(s session.State) {
	systray.SetTitle(iconFor(s))
}

func iconFor(s session.State) string {
	switch s {
	case session.Listening:
		return "🔴"
	case session.Flushing:
		return "✍️"
	default:
		return "🎙"
	}
}
