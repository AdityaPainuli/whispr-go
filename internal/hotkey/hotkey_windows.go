package hotkey

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Same semantics as the macOS tap: fire only on a clean Alt press→release.
// Any other key while Alt is down (Alt+Tab, Alt+F4...) cancels the tap, so
// normal shortcuts keep working. Implemented as a WH_KEYBOARD_LL hook —
// listen-only, every event passes through.

const (
	whKeyboardLL = 13
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105

	vkMenu  = 0x12 // generic Alt
	vkLMenu = 0xA4
	vkRMenu = 0xA5
)

type kbdllHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx   = user32.NewProc("CallNextHookEx")
	procGetMessage       = user32.NewProc("GetMessageW")

	firedCh     chan<- struct{}
	altDown     bool
	sawOtherKey bool
)

func isAlt(vk uint32) bool { return vk == vkMenu || vk == vkLMenu || vk == vkRMenu }

func hookProc(code int32, wparam uintptr, lparam uintptr) uintptr {
	if code >= 0 {
		k := (*kbdllHookStruct)(unsafe.Pointer(lparam))
		switch wparam {
		case wmKeyDown, wmSysKeyDown:
			if isAlt(k.VkCode) {
				if !altDown {
					altDown = true
					sawOtherKey = false
				}
			} else if altDown {
				sawOtherKey = true
			}
		case wmKeyUp, wmSysKeyUp:
			if isAlt(k.VkCode) && altDown {
				altDown = false
				if !sawOtherKey {
					select {
					case firedCh <- struct{}{}:
					default:
					}
				}
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, lparam)
	return ret
}

// Run installs the hook and blocks forever pumping messages.
// Sends on ch on every clean Alt tap.
func Run(ch chan<- struct{}) error {
	// The hook is tied to this thread's message queue; pin it.
	runtime.LockOSThread()

	firedCh = ch
	hook, _, err := procSetWindowsHookEx.Call(
		whKeyboardLL,
		windows.NewCallback(hookProc),
		0, 0,
	)
	if hook == 0 {
		return errors.New("hotkey: SetWindowsHookEx failed: " + err.Error())
	}

	// Low-level hooks need a message pump on the installing thread.
	var m msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 { // WM_QUIT or error
			return nil
		}
	}
}
