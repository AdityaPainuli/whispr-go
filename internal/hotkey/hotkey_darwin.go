package hotkey

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

extern void goHotkeyFired(void);

// Tap detection: fire only on a clean Option press→release. Any other key
// pressed while Option is down (Option+C, Option+arrows...) cancels the tap,
// so normal shortcuts keep working.
static bool optionDown = false;
static bool sawOtherKey = false;
static CFMachPortRef tap = NULL;

static CGEventRef tapCallback(CGEventTapProxy proxy, CGEventType type,
                              CGEventRef event, void *userInfo) {
	if (type == kCGEventFlagsChanged) {
		bool opt = (CGEventGetFlags(event) & kCGEventFlagMaskAlternate) != 0;
		if (opt && !optionDown) {
			optionDown = true;
			sawOtherKey = false;
		} else if (!opt && optionDown) {
			optionDown = false;
			if (!sawOtherKey) goHotkeyFired();
		}
	} else if (type == kCGEventKeyDown) {
		if (optionDown) sawOtherKey = true;
	} else if (type == kCGEventTapDisabledByTimeout ||
	           type == kCGEventTapDisabledByUserInput) {
		// macOS disables taps it thinks are slow; turn ours back on.
		CGEventTapEnable(tap, true);
	}
	return event; // listen-only: every event passes through untouched
}

// Returns 0 on success, -1 when the tap can't be created — which in
// practice means the Accessibility permission hasn't been granted.
static int startTap(void) {
	CGEventMask mask = CGEventMaskBit(kCGEventFlagsChanged) |
	                   CGEventMaskBit(kCGEventKeyDown);
	tap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap,
	                       kCGEventTapOptionListenOnly, mask, tapCallback, NULL);
	if (tap == NULL) return -1;
	CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(NULL, tap, 0);
	CFRunLoopAddSource(CFRunLoopGetCurrent(), src, kCFRunLoopCommonModes);
	CGEventTapEnable(tap, true);
	return 0;
}
*/
import "C"

import (
	"errors"
	"runtime"
)

var fired chan<- struct{}

// goHotkeyFired is the C→Go bridge (//export makes it C-visible). Runs on
// the run-loop thread: signal and get out.
//
//export goHotkeyFired
func goHotkeyFired() {
	select {
	case fired <- struct{}{}:
	default:
	}
}

// Run installs the event tap and blocks forever running the run loop.
// Call from the main goroutine. Sends on ch on every clean Option tap.
func Run(ch chan<- struct{}) error {
	// The tap's run-loop source lives on this thread's run loop; pin the
	// goroutine so the OS always finds it.
	runtime.LockOSThread()

	fired = ch
	if C.startTap() != 0 {
		return errors.New("hotkey: event tap failed — grant Accessibility permission " +
			"(System Settings → Privacy & Security → Accessibility) and restart")
	}
	C.CFRunLoopRun() // parks here for the app's lifetime
	return nil
}
