package output

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Paste puts text at the cursor of whatever app has focus.
// Same strategy as macOS: save clipboard → set clipboard → synthesize
// Ctrl+V → restore the old clipboard after the app has read the new one.

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	inputKeyboard  = 1
	keyeventfKeyup = 0x0002
	vkControl      = 0x11
	vkV            = 0x56
)

var (
	user32Out            = windows.NewLazySystemDLL("user32.dll")
	kernel32Out          = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard    = user32Out.NewProc("OpenClipboard")
	procCloseClipboard   = user32Out.NewProc("CloseClipboard")
	procEmptyClipboard   = user32Out.NewProc("EmptyClipboard")
	procGetClipboardData = user32Out.NewProc("GetClipboardData")
	procSetClipboardData = user32Out.NewProc("SetClipboardData")
	procSendInput        = user32Out.NewProc("SendInput")
	procGlobalAlloc      = kernel32Out.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32Out.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32Out.NewProc("GlobalUnlock")
)

type keybdInput struct {
	Type  uint32
	_     uint32 // struct alignment: INPUT is 8-aligned on 64-bit
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Extra uintptr
	_     [8]byte // pad to sizeof(INPUT) = 40 on 64-bit
}

func Paste(text string) error {
	old := readClipboard() // best-effort save

	if err := setClipboard(text); err != nil {
		return fmt.Errorf("output: set clipboard: %w", err)
	}
	pressCtrlV()

	// Ctrl+V is async — restore too early and the app pastes the OLD
	// contents. Same 150ms trade as macOS, off the hot path.
	if old != "" {
		go func(prev string) {
			time.Sleep(150 * time.Millisecond)
			_ = setClipboard(prev)
		}(old)
	}
	return nil
}

func readClipboard() string {
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p)))
}

func setClipboard(text string) error {
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return fmt.Errorf("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	size := uintptr(len(u16) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return fmt.Errorf("GlobalLock failed")
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u16))
	copy(dst, u16)
	procGlobalUnlock.Call(h)

	// On success the system owns the memory; do not free it.
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		return fmt.Errorf("SetClipboardData failed")
	}
	return nil
}

func pressCtrlV() {
	inputs := []keybdInput{
		{Type: inputKeyboard, Vk: vkControl},
		{Type: inputKeyboard, Vk: vkV},
		{Type: inputKeyboard, Vk: vkV, Flags: keyeventfKeyup},
		{Type: inputKeyboard, Vk: vkControl, Flags: keyeventfKeyup},
	}
	procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
}
