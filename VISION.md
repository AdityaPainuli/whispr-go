# Vision

## The bet

Dictation should feel like typing, except faster. Today you pick two out of three: fast, accurate, private. Cloud tools are fast and accurate. Local tools are private and accurate. Nobody ships all three.

The reason is architectural, not fundamental. Local tools batch: record, then decode, then paste. The wait scales with how long you talked. Cloud tools stream, so the wait is constant and small, but you ship your voice off-device and pay rent for it.

Streaming-native local models ended that tradeoff. A 0.6b cache-aware transducer decodes faster than real time on a base M2 with no GPU tricks. The decode happens while you talk. When you stop, only the tail is left.

So the bet: **a local dictation app built streaming-first beats the cloud tools on latency, matches them on quality, and never sends audio anywhere.**

## Targets

- Stop-to-paste under 300ms on Apple Silicon. Measured, not vibes.
- Quality in the same class as cloud streaming APIs (6-7% real-world WER), with punctuation from the model itself.
- One binary. No Electron, no Python, no sidecar servers, no temp files.
- Idle cost near zero: no mic held open, no orange dot, model prewarmed once.

## Principles

1. **Latency is the product.** Every design decision gets judged by what it does to stop-to-paste. Features that add a hop lose.
2. **Local only.** Not local-first, local-only. The moment audio can leave the machine, the privacy story is marketing.
3. **Core is portable, adapters are native.** Platform-agnostic Go core, thin per-OS layers for hotkey, paste, capture. macOS first because that's my machine, Windows/Linux when the core is proven.
4. **Boring code over clever code.** Interfaces where a second implementation is plausible, none where it isn't. The state machine is testable without hardware. That stays true.

## Roadmap, roughly

1. Core loop: hotkey → talk → paste. Prove the 300ms number end to end.
2. Menu bar app, model downloader, first release.
3. Live partials as you talk (the engine already produces them).
4. Multilingual via Nemotron 3.5 streaming (40 locales, same runtime).
5. Windows and Linux adapters.
6. Maybe: optional local LLM cleanup pass for tone/formatting. Only if it can stay under the latency budget.

## What this is not

- Not a Wispr Flow clone with the cloud swapped out. Streaming-first changes the whole pipeline shape.
- Not a transcription suite. No meeting bots, no file batch jobs, no notes app. Dictation, done extremely well.
- Not a subscription. It's my machine doing the work.
