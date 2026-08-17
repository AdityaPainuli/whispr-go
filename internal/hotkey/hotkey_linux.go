package hotkey

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Same semantics as the macOS tap: fire only on a clean Alt press→release;
// any other key (or mouse button) while Alt is down cancels the tap.
//
// Implemented by reading the kernel's evdev devices directly — no X11, no
// Wayland protocol, works on both. The cost: the user must be able to read
// /dev/input/event* (usually: add yourself to the `input` group).

const (
	evKey       = 1
	keyLeftAlt  = 56
	keyRightAlt = 100
)

// input_event on 64-bit: struct timeval (16 bytes) + type u16 + code u16 + value s32
const eventSize = 24

type tapState struct {
	mu          sync.Mutex
	altDown     bool
	sawOtherKey bool
	fired       chan<- struct{}
}

func (t *tapState) handle(code uint16, value int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	isAlt := code == keyLeftAlt || code == keyRightAlt
	switch {
	case isAlt && value == 1: // down
		if !t.altDown {
			t.altDown = true
			t.sawOtherKey = false
		}
	case isAlt && value == 0: // up
		if t.altDown {
			t.altDown = false
			if !t.sawOtherKey {
				select {
				case t.fired <- struct{}{}:
				default:
				}
			}
		}
	case value == 1 && t.altDown: // any other key/button pressed mid-tap
		t.sawOtherKey = true
	}
}

// Run opens every readable input device and blocks forever.
// Sends on ch on every clean Alt tap.
func Run(ch chan<- struct{}) error {
	devices, _ := filepath.Glob("/dev/input/event*")
	state := &tapState{fired: ch}

	opened := 0
	for _, dev := range devices {
		f, err := os.Open(dev)
		if err != nil {
			continue // not readable; likely not in the input group
		}
		opened++
		go func(f *os.File) {
			defer f.Close()
			buf := make([]byte, eventSize)
			for {
				if _, err := readFull(f, buf); err != nil {
					return // device unplugged
				}
				typ := binary.LittleEndian.Uint16(buf[16:18])
				if typ != evKey {
					continue
				}
				code := binary.LittleEndian.Uint16(buf[18:20])
				value := int32(binary.LittleEndian.Uint32(buf[20:24]))
				state.handle(code, value)
			}
		}(f)
	}
	if opened == 0 {
		return errors.New("hotkey: no readable input devices — add your user to the " +
			"`input` group (sudo usermod -aG input $USER) and log back in")
	}
	select {} // park forever; the per-device goroutines do the work
}

func readFull(f *os.File, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := f.Read(buf[n:])
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}
