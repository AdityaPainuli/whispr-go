package output

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// Synthesizes Cmd+V: key-down then key-up, both carrying the Command flag.
// Down without up = V held forever, so the pair is non-negotiable.
static void pressCmdV(void) {
	CGEventRef down = CGEventCreateKeyboardEvent(NULL, 9, true); // 9 = kVK_ANSI_V
	CGEventSetFlags(down, kCGEventFlagMaskCommand);
	CGEventPost(kCGHIDEventTap, down);
	CFRelease(down);

	CGEventRef up = CGEventCreateKeyboardEvent(NULL, 9, false);
	CGEventSetFlags(up, kCGEventFlagMaskCommand);
	CGEventPost(kCGHIDEventTap, up);
	CFRelease(up);
}
*/
import "C"

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Paste puts text at the cursor of whatever app has focus.
// Strategy: save clipboard → set clipboard to text → synthesize Cmd+V →
// restore the old clipboard after the frontmost app has read the new one.
// Needs the Accessibility permission; without it CGEventPost silently no-ops.
func Paste(text string) error {
	old, _ := exec.Command("pbpaste").Output() // best-effort save

	if err := setClipboard(text); err != nil {
		return fmt.Errorf("output: set clipboard: %w", err)
	}
	C.pressCmdV()

	// Cmd+V is async — the app reads the clipboard a beat after the event
	// lands. Restore too early and it pastes the OLD contents. 150ms is
	// crude but reliable; do it off the hot path so Paste returns fast.
	if len(old) > 0 {
		go func(prev string) {
			time.Sleep(150 * time.Millisecond)
			_ = setClipboard(prev)
		}(string(old))
	}
	return nil
}

func setClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
