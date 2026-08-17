package tray

import (
	_ "embed"
	"runtime"

	"fyne.io/systray"

	"github.com/AdityaPainuli/whispr-go/internal/session"
)

// macOS shows the emoji title, so the state is visible at a glance and no
// icon is needed. Windows shows ONLY the icon (SetTitle is a no-op there),
// so without one the app is invisible — the "is it even running?" bug.
// Windows wants .ico bytes, linux wants .png.

//go:embed icons/idle.png
var idlePNG []byte

//go:embed icons/rec.png
var recPNG []byte

//go:embed icons/idle.ico
var idleICO []byte

//go:embed icons/rec.ico
var recICO []byte

func stateIcon(recording bool) []byte {
	switch runtime.GOOS {
	case "windows":
		if recording {
			return recICO
		}
		return idleICO
	default:
		if recording {
			return recPNG
		}
		return idlePNG
	}
}

// Run owns the tray/menu bar item. Must be called from the main goroutine
// and blocks until Quit is chosen. onReady fires once the item exists —
// wire callbacks there. onExit fires after Quit for cleanup.
func Run(onReady func(), onExit func()) {
	systray.Run(func() {
		if runtime.GOOS == "darwin" {
			systray.SetTitle(iconFor(session.Idle))
		} else {
			systray.SetIcon(stateIcon(false))
		}
		systray.SetTooltip("whispr — local dictation")

		quit := systray.AddMenuItem("Quit whispr", "Stop dictation and exit")
		go func() {
			<-quit.ClickedCh
			systray.Quit()
		}()

		onReady()
	}, onExit)
}

// SetState updates the tray. Safe to call from any goroutine — systray
// marshals to the UI thread internally.
func SetState(s session.State) {
	if runtime.GOOS == "darwin" {
		systray.SetTitle(iconFor(s))
	} else {
		systray.SetIcon(stateIcon(s == session.Listening))
	}
	switch s {
	case session.Listening:
		systray.SetTooltip("whispr — listening")
	case session.Flushing:
		systray.SetTooltip("whispr — finishing up")
	default:
		systray.SetTooltip("whispr — tap Alt/Option to dictate")
	}
}

// SetStatus shows arbitrary text — download progress, startup errors.
// Title on mac; tooltip elsewhere (Windows has no title).
func SetStatus(text string) {
	if runtime.GOOS == "darwin" {
		systray.SetTitle(text)
	}
	systray.SetTooltip(text)
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
