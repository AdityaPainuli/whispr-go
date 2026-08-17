package output

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Paste puts text at the cursor of whatever app has focus.
// Same strategy as macOS: save clipboard → set clipboard → synthesize
// Ctrl+V → restore. Linux has no single clipboard API, so this shells out
// to the standard tools per display server:
//
//	Wayland: wl-copy / wl-paste (wl-clipboard) + wtype
//	X11:     xclip + xdotool
//
// Missing tools produce a clear install hint instead of a silent no-op.

func wayland() bool { return os.Getenv("WAYLAND_DISPLAY") != "" }

func need(tool, pkg string) error {
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("output: %s not found — install %s", tool, pkg)
	}
	return nil
}

func Paste(text string) error {
	var copyTool, pasteTool, pkg string
	var typeCmd []string
	if wayland() {
		copyTool, pasteTool, pkg = "wl-copy", "wl-paste", "wl-clipboard + wtype"
		typeCmd = []string{"wtype", "-M", "ctrl", "-k", "v", "-m", "ctrl"}
	} else {
		copyTool, pasteTool, pkg = "xclip", "xclip", "xclip + xdotool"
		typeCmd = []string{"xdotool", "key", "--clearmodifiers", "ctrl+v"}
	}
	if err := need(copyTool, pkg); err != nil {
		return err
	}
	if err := need(typeCmd[0], pkg); err != nil {
		return err
	}

	old := readClipboard(pasteTool) // best-effort save

	if err := setClipboard(copyTool, text); err != nil {
		return fmt.Errorf("output: set clipboard: %w", err)
	}
	if err := exec.Command(typeCmd[0], typeCmd[1:]...).Run(); err != nil {
		return fmt.Errorf("output: synthesize ctrl+v: %w", err)
	}

	// Ctrl+V is async — restore too early and the app pastes the OLD
	// contents. Same 150ms trade as macOS, off the hot path.
	if old != "" {
		go func(prev string) {
			time.Sleep(150 * time.Millisecond)
			_ = setClipboard(copyTool, prev)
		}(old)
	}
	return nil
}

func readClipboard(tool string) string {
	var cmd *exec.Cmd
	if tool == "wl-paste" {
		cmd = exec.Command("wl-paste", "--no-newline")
	} else {
		cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func setClipboard(tool, text string) error {
	var cmd *exec.Cmd
	if tool == "wl-copy" {
		cmd = exec.Command("wl-copy")
	} else {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
