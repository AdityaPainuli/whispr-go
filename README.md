# whispr-go

Local dictation for your desktop. Press a hotkey, talk, text lands at your cursor.

No cloud. No audio leaving your machine. And faster than the cloud tools, because the model transcribes while you're still talking.

## Why I'm building this

I dug into how existing dictation apps work. The gap isn't the model, it's the architecture.

Most local tools (OpenWhispr, superwhisper on Whisper models) record everything first, then decode the whole file after you stop. On a laptop CPU that's 1-3 seconds of waiting. Cloud tools like Wispr Flow stream to a server while you talk, so they feel instant, but your voice goes to their machines and you pay monthly for it.

Streaming-native local models close that gap now. NVIDIA's Nemotron streaming transducer decodes on-device faster than real time on a base M2. I measured it:

- RTF 0.39 on an M2, CPU only, 4 threads
- ~240ms from stop-of-speech to final text
- punctuation and capitalization come out of the model directly
- under 1GB RAM for the ASR, ~450MB model on disk

On top of that sits a small local LLM that cleans up what you said, and it runs *while you're still talking*. More on that below.

## Can I use it today?

Honest answer: only if you're comfortable with a terminal and Go. There's no downloadable app yet.

That's the current milestone. The two issues that get us there are [#7 (.app bundle)](https://github.com/AdityaPainuli/whispr-go/issues/7) and [#8 (model downloader + first run)](https://github.com/AdityaPainuli/whispr-go/issues/8). When those close, this becomes: `brew install`, open it, grant two permissions, talk. Free, no account, no subscription — it's your machine doing the work.

If that's you, watch the repo. If you're fine with a terminal, jump to [Run it from source](#run-it-from-source).

## How it works

```
mic (malgo/CoreAudio) → channel → feeder goroutine → sherpa-onnx (cgo) → cleanup LLM → paste at cursor
```

No temp files, no IPC for audio. Samples go from the OS callback straight into the model's C API in-process.

The cleanup pass is the interesting part. The ASR detects when you pause between sentences. Each finished sentence gets shipped to a local LLM (llama.cpp subprocess) for cleanup *while you speak the next one*. When you stop, only the last sentence still needs cleaning. The wait at the end stays constant no matter how long you talked. Same streaming-first trick as the ASR, applied to the LLM.

Cleanup fixes punctuation, splits run-ons, strips filler words (um, uh, you know). On machines with 16GB+ RAM it also applies spoken self-corrections: "let's do the meeting at 4pm, no wait, make it 5" pastes as "Let's do the meeting at 5pm." That needs a 3B model, and a 3B resident on an 8GB machine starves the ASR (measured, badly), so 8GB machines get a smaller model and skip corrections. The app picks automatically at launch.

If the cleanup model is missing or slow, the raw transcript pastes instead. Cleanup can improve a dictation, never delay or lose one.

- `internal/engine` — streaming ASR + endpoint detection. sherpa-onnx cgo binding (Nemotron Speech Streaming EN 0.6b, int8)
- `internal/audio` — mic capture at 16kHz mono via malgo, buffered channel as shock absorber
- `internal/refine` — cleanup LLM behind an interface. llama.cpp server subprocess, prompt is the whole product
- `internal/session` — dictation state machine, segment pipeline, teardown ordering. Unit-tested with fakes
- `cmd/enginetest` — feeds a WAV through the engine, prints partials + flush timing
- `cmd/mictest` — live dictation in the terminal, no permissions needed
- `cmd/segtest` — end-to-end harness: WAV through the real session + engine + LLM, no mic
- `cmd/refinetest` — cleanup latency measurements

## Status

- [x] Streaming engine: decode while talking, partials, truncation-safe flush
- [x] Mic capture
- [x] Session state machine + tests
- [x] Global hotkey: tap Option anywhere
- [x] Paste at cursor, old clipboard restored
- [x] Menu bar app: 🎙 idle, 🔴 listening, ✍️ flushing
- [x] Cleanup layer: pause-segmented, runs during speech, tail-only wait at stop
- [x] Self-corrections (16GB+ machines, RAM-gated automatically)
- [ ] .app bundle, shipped free via Homebrew ([#7](https://github.com/AdityaPainuli/whispr-go/issues/7))
- [ ] Model downloader + first-run walkthrough ([#8](https://github.com/AdityaPainuli/whispr-go/issues/8))
- [ ] Settings / config file ([#3](https://github.com/AdityaPainuli/whispr-go/issues/3))
- [ ] Windows/Linux ([#13](https://github.com/AdityaPainuli/whispr-go/issues/13))

All known gaps live in the [issues](https://github.com/AdityaPainuli/whispr-go/issues). macOS (Apple Silicon) first. The core is platform-agnostic Go, platform adapters come later.

## Run it from source

Needs Go 1.21+, macOS on Apple Silicon, and ~2GB of downloads (ASR runtime + models, none of it in the repo).

```bash
# 1. sherpa-onnx runtime (ASR)
curl -sL -o sherpa.tar.bz2 https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.4/sherpa-onnx-v1.13.4-osx-universal2-shared.tar.bz2
tar xjf sherpa.tar.bz2
mkdir -p third_party/sherpa
cp -r sherpa-onnx-v1.13.4-osx-universal2-shared/lib third_party/sherpa/lib
cp -r sherpa-onnx-v1.13.4-osx-universal2-shared/include third_party/sherpa/include

# 2. ASR model (~450MB)
curl -sL -o nemotron.tar.bz2 https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemotron-speech-streaming-en-0.6b-560ms-int8-2026-04-25.tar.bz2
tar xjf nemotron.tar.bz2
mkdir -p models
mv sherpa-onnx-nemotron-speech-streaming-en-0.6b-560ms-int8-2026-04-25 models/nemotron-en

# 3. llama.cpp server (cleanup LLM runtime)
# grab the latest macos-arm64 build from https://github.com/ggml-org/llama.cpp/releases
# copy llama-server and its .dylib files into third_party/llama/

# 4. cleanup model (~1.1GB; every machine needs this one)
curl -sL -o models/qwen2.5-1.5b-instruct-q4_k_m.gguf "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/qwen2.5-1.5b-instruct-q4_k_m.gguf?download=true"

# 5. only if you have 16GB+ RAM: the corrections model (~2.1GB)
curl -sL -o models/qwen2.5-3b-instruct-q4_k_m.gguf "https://huggingface.co/Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q4_k_m.gguf?download=true"

# 6. macOS kills unsigned downloaded dylibs, re-sign them
codesign --force -s - third_party/sherpa/lib/*.dylib third_party/llama/*.dylib third_party/llama/llama-server

# talk to it
go run ./cmd/app
```

Tap Option anywhere to start/stop. First run needs two grants for your terminal: microphone (auto-prompted) and Accessibility (System Settings → Privacy & Security → Accessibility) for the hotkey tap + paste. `cmd/mictest` works terminal-only without permissions. Skipping steps 3-5 is fine too: the app warns and dictates raw ASR output, which already has punctuation.

## Tests

```bash
go test ./...
```

Session and refine tests run against fakes, no hardware or models needed. `go run ./cmd/segtest` runs the full pipeline (real models required) against a test WAV and prints per-segment cleanup timing.
