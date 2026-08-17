package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"
	"github.com/AdityaPainuli/whispr-go/internal/engine"
)

const chunkSamples = 8960

func main() {
	eng, err := engine.NewSherpa(engine.Config{
		EncoderPath: "models/nemotron-en/encoder.int8.onnx",
		DecoderPath: "models/nemotron-en/decoder.int8.onnx",
		JoinerPath:  "models/nemotron-en/joiner.int8.onnx",
		TokensPath:  "models/nemotron-en/tokens.txt",
		NumThreads:  4,
	})
	if err != nil {
		panic(err)
	}
	defer eng.Close()

	samples := readWAV("testdata/test_speech.wav")

	st, _ := eng.NewStream(nil)
	defer st.Close()

	last := ""
	for off := 0; off < len(samples); off += chunkSamples {
		end := min(off+chunkSamples, len(samples))
		st.Feed(samples[off:end])
		if p, _ := st.Partial(); p != last {
			fmt.Printf("[partial] %s\n", p)
			last = p
		}
	}

	t0 := time.Now()
	final, _ := st.Flush()
	fmt.Printf("\n[Final in %v] %s\n", time.Since(t0), final)
}

// naive 16kHz mono s16le reader - skips the 44 byte header.
// Fine for our afconverter file; real WAV parsing comes with the audio milestone.
func readWAV(path string) []int16 {
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	data := raw[44:]
	out := make([]int16, len(data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return out
}
