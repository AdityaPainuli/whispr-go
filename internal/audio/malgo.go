package audio

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"github.com/gen2brain/malgo"
)

// Capture owns the mic. Create once; Start/Stop per dictation.
type Capture interface {
	// Opens the default mic at 16kHz mono s16 and returns a channel of
	// sample buffers. Channel closes when Stop is called.
	Start() (<-chan []int16, error)
	Stop() error
	Close() error // release the device
}

type malgoCapture struct {
	ctx *malgo.AllocatedContext
	dev *malgo.Device
	ch  chan []int16
	// drops counts callback buffers thrown away because the channel was
	// full — the feeder couldn't keep up. Nonzero drops = missing audio =
	// garbled transcripts. Prime suspect under memory pressure.
	drops atomic.Int64
}

// New initializes the audio backend (CoreAudio on macOS). Once per app
func New() (Capture, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init context: %w", err)
	}
	// Say which mic we're on: Bluetooth mics (AirPods) drop to a low-quality
	// codec while capturing and transcription craters — first thing to check
	// when a transcript comes back garbled.
	if infos, err := ctx.Devices(malgo.Capture); err == nil {
		for _, info := range infos {
			if info.IsDefault != 0 {
				fmt.Printf("[audio] capture device: %s\n", info.Name())
			}
		}
	}
	return &malgoCapture{ctx: ctx}, nil
}

func (c *malgoCapture) Start() (<-chan []int16, error) {
	if c.dev != nil {
		return nil, fmt.Errorf("audio: already capturing")
	}

	// 1024 slots ≈ 10s of audio (~320KB). Measured: memory pressure can
	// slow ASR decode 17x; with a 16-slot buffer that meant dropped mic
	// frames = corrupted words. A big buffer converts overload into a
	// delayed paste instead of a wrong one.
	c.ch = make(chan []int16, 1024)

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = 16000

	onData := func(_, pInput []byte, frameCount uint32) {
		samples := make([]int16, frameCount)
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(pInput[i*2:]))
		}

		select {
		case c.ch <- samples:
		default:
			c.drops.Add(1)
		}
	}
	dev, err := malgo.InitDevice(c.ctx.Context, cfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		close(c.ch)
		return nil, fmt.Errorf("audio: init device: %w", err)
	}

	if err := dev.Start(); err != nil {
		dev.Uninit()
		close(c.ch)
		return nil, fmt.Errorf("audio: start device: %w", err)
	}
	c.dev = dev
	return c.ch, nil

}

func (c *malgoCapture) Stop() error {
	if c.dev == nil {
		return nil
	}

	c.dev.Stop()
	c.dev.Uninit()
	c.dev = nil
	close(c.ch)
	if n := c.drops.Swap(0); n > 0 {
		fmt.Printf("[audio] WARNING: dropped %d buffers (~%dms of speech) — feeder too slow\n", n, n*10)
	}
	return nil
}

func (c *malgoCapture) Close() error {
	_ = c.Stop()
	return c.ctx.Uninit()
}
