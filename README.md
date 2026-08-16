# whispr-go

Local dictation for your desktop. Press a hotkey, talk, text lands at your cursor.

No cloud. No audio leaving your machine. And faster than the cloud tools, because the model transcribes while you're still talking.

## Why I'm building this

I dug into how existing dictation apps work. The gap isn't the model, it's the architecture.

Most local tools (OpenWhispr, superwhisper on Whisper models) record everything first, then decode the whole file after you stop. On a laptop CPU that's 1-3 seconds of waiting. Cloud tools like Wispr Flow stream to a server while you talk, so they feel instant, but your voice goes to their machines and you pay monthly for it.

Streaming-native local models close that gap now. NVIDIA's Nemotron streaming transducer decodes on-device faster than real time on a base M2. I measured it:

- RTF 0.39 on an M2, CPU only, 4 threads
- ~240ms from stop-of-speech to final text
- punctuation and capitalization come out of the model directly, no cleanup pass
- under 1GB RAM, ~450MB model on disk

Target: under 300ms stop-to-paste, fully offline. That beats Wispr Flow's 1-3s round trip.

## How it works

```
mic (malgo/CoreAudio) → channel → feeder goroutine → sherpa-onnx (cgo) → paste at cursor
```

No temp files, no IPC, no sidecar processes. Audio goes from the OS callback straight into the model's C API in-process. That's the whole trick.

- `internal/engine` — streaming ASR. Interface + sherpa-onnx cgo binding (Nemotron Speech Streaming EN 0.6b, int8)
- `internal/audio` — mic capture at 16kHz mono via malgo, real-time-safe callback, buffered channel as shock absorber
- `internal/session` — dictation state machine (Idle → Listening → Flushing), owns teardown ordering, unit-tested with fakes
- `cmd/enginetest` — feeds a WAV through the engine, prints partials + flush timing
- `cmd/mictest` — live dictation in the terminal, Enter to start/stop

## Status

- [x] Engine binding: streaming decode, partials, truncation-safe flush
- [x] Mic capture
- [x] Session state machine + tests
- [x] Global hotkey: tap Option anywhere (CGEventTap, combos pass through untouched)
- [x] Paste at cursor: clipboard + synthetic Cmd+V, old clipboard restored
- [x] Menu bar app: 🎙 idle, 🔴 listening, ✍️ flushing
- [ ] Cleanup layer (punctuation/disfluencies) under the latency budget
- [ ] .app bundle + codesign
- [ ] Model downloader, settings, Windows/Linux

macOS (Apple Silicon) first. The core is platform-agnostic Go, platform adapters come later.

## Run it

Needs Go 1.21+, macOS, and the model + sherpa-onnx runtime (not in the repo, ~500MB):

```bash
# runtime libs + headers
curl -sL -o sherpa.tar.bz2 https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.4/sherpa-onnx-v1.13.4-osx-universal2-shared.tar.bz2
tar xjf sherpa.tar.bz2
mkdir -p third_party/sherpa
cp -r sherpa-onnx-v1.13.4-osx-universal2-shared/lib third_party/sherpa/lib
cp -r sherpa-onnx-v1.13.4-osx-universal2-shared/include third_party/sherpa/include

# model
curl -sL -o nemotron.tar.bz2 https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemotron-speech-streaming-en-0.6b-560ms-int8-2026-04-25.tar.bz2
tar xjf nemotron.tar.bz2
mkdir -p models
mv sherpa-onnx-nemotron-speech-streaming-en-0.6b-560ms-int8-2026-04-25 models/nemotron-en

# macOS kills unsigned downloaded dylibs, re-sign them
codesign --force -s - third_party/sherpa/lib/*.dylib

# talk to it
go run ./cmd/app
```

Tap Option anywhere to start/stop. First run needs two grants for your terminal: microphone (auto-prompted) and Accessibility (System Settings → Privacy & Security → Accessibility) for the hotkey tap + paste. `cmd/mictest` still works for terminal-only testing without permissions.

## Tests

```bash
go test ./...
```

Session tests run against fake engine/mic, no hardware needed.
