package engine

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/sherpa/include
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/sherpa/lib -lsherpa-onnx-c-api -Wl,-rpath,${SRCDIR}/../../third_party/sherpa/lib
#include <stdlib.h>
#include "sherpa-onnx/c-api/c-api.h"
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"
)

type sherpaEngine struct {
	rec *C.SherpaOnnxOnlineRecognizer
}

func NewSherpa(cfg Config) (Engine, error) {
	// C.CString allocates on the C heap — Go's GC never sees it, so every
	// one of these needs a matching C.free or it leaks.
	enc := C.CString(cfg.EncoderPath)
	defer C.free(unsafe.Pointer(enc))
	dec := C.CString(cfg.DecoderPath)
	defer C.free(unsafe.Pointer(dec))
	join := C.CString(cfg.JoinerPath)
	defer C.free(unsafe.Pointer(join))
	tok := C.CString(cfg.TokensPath)
	defer C.free(unsafe.Pointer(tok))
	method := C.CString("greedy_search")
	defer C.free(unsafe.Pointer(method))

	// var of a C struct type is zero-initialized by Go — important, sherpa
	// treats zero/empty fields as "not used" (paraformer, ctc, lm...).
	var c C.SherpaOnnxOnlineRecognizerConfig
	c.model_config.transducer.encoder = enc
	c.model_config.transducer.decoder = dec
	c.model_config.transducer.joiner = join
	c.model_config.tokens = tok
	c.model_config.num_threads = C.int(cfg.NumThreads)
	c.decoding_method = method
	c.feat_config.sample_rate = 16000
	c.feat_config.feature_dim = 80

	rec := C.SherpaOnnxCreateOnlineRecognizer(&c)
	if rec == nil {
		return nil, errors.New("sherpa: recognizer creation failed (check model paths)")
	}
	return &sherpaEngine{rec: rec}, nil
}

func (e *sherpaEngine) NewStream(_ context.Context) (Stream, error) {
	st := C.SherpaOnnxCreateOnlineStream(e.rec)
	if st == nil {
		return nil, errors.New("sherpa: stream creation failed")
	}
	return &sherpaStream{eng: e, st: st, buf: make([]C.float, 0, 16000)}, nil
}

func (e *sherpaEngine) Close() error {
	C.SherpaOnnxDestroyOnlineRecognizer(e.rec)
	return nil
}

type sherpaStream struct {
	eng *sherpaEngine
	st  *C.SherpaOnnxOnlineStream
	buf []C.float // reused scratch: avoids allocating on every Feed
}

func (s *sherpaStream) Feed(samples []int16) error {
	// sherpa wants float32 in [-1, 1]; mic gives s16. Convert into the
	// reused buffer. This runs on the audio path — no allocations after warmup.
	s.buf = s.buf[:0]
	for _, v := range samples {
		s.buf = append(s.buf, C.float(v)/32768.0)
	}
	// &s.buf[0] hands C a pointer into Go memory. Legal because sherpa
	// copies synchronously inside the call and keeps no reference —
	// the one cgo pointer rule that matters: C must not retain it.
	C.SherpaOnnxOnlineStreamAcceptWaveform(s.st, 16000, &s.buf[0], C.int32_t(len(s.buf)))
	s.decodePending()
	return nil
}

// decodePending runs the decoder for every chunk that has enough audio.
// This is where the "transcribe while talking" actually happens.
func (s *sherpaStream) decodePending() {
	for C.SherpaOnnxIsOnlineStreamReady(s.eng.rec, s.st) == 1 {
		C.SherpaOnnxDecodeOnlineStream(s.eng.rec, s.st)
	}
}

func (s *sherpaStream) Partial() (string, error) {
	r := C.SherpaOnnxGetOnlineStreamResult(s.eng.rec, s.st)
	defer C.SherpaOnnxDestroyOnlineRecognizerResult(r)
	return C.GoString(r.text), nil
}

func (s *sherpaStream) Flush() (string, error) {
	// The model's 560ms chunk + right-context lookahead means the last words
	// are still inside an unfilled window at end of speech. Feed tail silence
	// so they flush out — without this the final transcript gets truncated.
	tail := make([]C.float, 4800) // 0.3s of zeros at 16kHz — smallest padding that doesn't truncate
	C.SherpaOnnxOnlineStreamAcceptWaveform(s.st, 16000, &tail[0], C.int32_t(len(tail)))
	C.SherpaOnnxOnlineStreamInputFinished(s.st)
	s.decodePending()
	return s.Partial()
}

func (s *sherpaStream) Close() error {
	C.SherpaOnnxDestroyOnlineStream(s.st)
	return nil
}
