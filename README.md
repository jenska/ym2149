# YM2149

Cycle-accurate YM2149F / Atari ST PSG emulation in Go.

Release target: `v1.0.0`

This repository is intended to be reused later as the sound subsystem for a larger Atari ST emulator. The current focus is a reusable chip core with deterministic timing, backend-neutral audio rendering helpers, an Ebiten adapter, and a demo harness for quick listening and debugging.

## Versioning

- The root module path is `github.com/jenska/ym2149`.
- The planned first stable release is `v1.0.0`.
- This repository currently uses a single Go module. All packages under `emulation/`, `renderer/`, `internal/`, and `cmd/` are released together under the root module tag.

## Installation

```sh
go get github.com/jenska/ym2149@v1.0.0
```

## Status

- YM2149F-oriented core with all 16 PSG registers
- Clock-driven stepping API for emulator integration
- Tone, noise, envelope, mixer, and I/O port handling
- YM2149-style nonlinear analog mix table for mono PCM generation
- Ebiten audio adapter that exposes stereo `float32` PCM to `audio.NewPlayerF32`
- Scripted and interactive demo scaffolding

## Package Layout

- `emulation`: reusable PSG core package
- `renderer/atarist`: Atari ST board-output approximation
- `renderer/audiostream`: backend-neutral mono→stereo and interleaved-stereo PCM readers
- `renderer/bandlimited`: oversampling + FIR decimation renderer
- `renderer/stereo`: per-channel panning (ABC / ACB / mono) into a stereo pair
- `renderer/ebitenaudio`: Ebiten audio reader/player helpers
- `format/ym`: YM2!/YM3!/YM3b/YM5!/YM6! music-file decoder and replayer
- `internal/psgdemo`: shared scripted demo logic
- `cmd/psgdemo`: Ebiten demo app

## Design Notes

- The chip is stepped in master clock cycles via `Step(cycles)`.
- Internal tone, noise, and envelope generators advance on the YM2149's `/8` internal timing domain.
- Audio is rendered inside the core into a mono `float32` buffer at a configurable sample rate.
- Channel mixing uses a measured-style YM2149 resistor-network model instead of a simple digital average.
- `renderer/bandlimited` can decimate an oversampled mono stream through a FIR low-pass before host playback.
- `renderer/atarist` adds a simple ST-style board stage with AC coupling and treble roll-off.
- The Ebiten adapter duplicates mono PCM to stereo because Ebiten's `NewPlayerF32` expects stereo data.
- Exact per-revision Atari ST board measurements and output coloration are still intentionally deferred.

## Quick Start

```go
package main

import (
	"log"

	ym2149 "github.com/jenska/ym2149/emulation"
	"github.com/jenska/ym2149/renderer/atarist"
	"github.com/jenska/ym2149/renderer/bandlimited"
)

func main() {
	chip := ym2149.NewWithDefaults(2_000_000, 48_000*4)

	chip.SelectRegister(0)
	chip.WriteData(0x20)
	chip.SelectRegister(1)
	chip.WriteData(0x01)

	chip.SelectRegister(7)
	chip.WriteData(0x3e)
	chip.SelectRegister(8)
	chip.WriteData(0x0f)

	chip.Step(20_000)

	decimator, err := bandlimited.New(chip, bandlimited.Config{
		OversampleFactor: 4,
	})
	if err != nil {
		log.Fatal(err)
	}

	board := atarist.New(decimator, atarist.Config{})
	samples := make([]float32, decimator.OutputSampleRate()/10)
	n := board.DrainMonoF32(samples)
	_ = samples[:n]
}
```

## Core API

The `github.com/jenska/ym2149/emulation` package currently exposes:

- `New(Config) *Chip`
- `NewWithDefaults(clockHz, sampleRate) *Chip`
- `NewClockDomain(sourceHz, targetHz) *ClockDomain`
- `NewPSGClockDomain(hostHz, psgHz) *ClockDomain`
- `(*Chip).Reset()`
- `(*Chip).Step(cycles uint32)`
- `(*Chip).Cycles() uint64`
- `(*Chip).ClockHz() int`
- `(*Chip).OutputSampleRate() int`
- `(*Chip).BufferedSamples() int`
- `(*Chip).SelectRegister(reg byte)`
- `(*Chip).WriteData(v byte)`
- `(*Chip).ReadData() byte`
- `(*Chip).Write(reg, value byte)` — immediate register write, no latch
- `(*Chip).WriteAt(atCycle uint64, reg, value byte)` — schedule a write
- `(*Chip).PendingWrites() int`
- `(*Chip).ClearPendingWrites()`
- `(*Chip).SetPortAInput(v byte)`
- `(*Chip).SetPortBInput(v byte)`
- `(*Chip).Ports() Ports`
- `(*Chip).DrainMonoF32(dst []float32) int`
- `(*Chip).DrainChannelF32(ch int, dst []float32) int` — per-channel PCM (needs `Config.ChannelTaps`)
- `(*Chip).ChannelTapsEnabled() bool`
- `(*Chip).BufferedChannelSamples() int`

The library is safe to call from concurrent goroutines, which keeps it practical for a future emulator thread driving chip state while an audio thread drains samples.

For host timing, `ClockDomain` provides a tiny exact integer accumulator that converts one cycle domain into another without losing fractional progress across calls.

### Timestamped bus writes

A host that drives the PSG from a CPU/bus model can queue register writes with
exact sub-`Step` timing instead of interleaving many small `Step` calls:

```go
base := chip.Cycles()
chip.WriteAt(base+0,   7, 0x38) // mixer, at the start of the block
chip.WriteAt(base+112, 0, 0x2e) // period low, 112 cycles later
chip.WriteAt(base+112, 1, 0x01)
chip.Step(40_000)               // advance once; writes land at their cycle
```

Writes are applied in scheduling order at the top of their target cycle,
before that cycle is integrated. A write whose cycle already passed is applied
at the start of the next `Step`. `Reset` clears the queue.

### Per-channel taps

With `Config.ChannelTaps` the core also buffers each tone channel's isolated
contribution (drained with `DrainChannelF32`), which `renderer/stereo` pans
into a stereo image. The three channel levels do not sum exactly to the mono
output because the output stage is modelled as a non-linear resistor network.

## Band-Limited Renderer

The `github.com/jenska/ym2149/renderer/bandlimited` package downsamples an oversampled mono source through a windowed-sinc FIR:

- `bandlimited.New(source, config)`
- `(*bandlimited.Decimator).DrainMonoF32(dst)`
- `(*bandlimited.Decimator).OutputSampleRate()`

Typical usage is:

1. Configure the chip to produce PCM at `targetSampleRate * oversampleFactor`.
2. Wrap it with `renderer/bandlimited`.
3. Pass the decimated output into `renderer/atarist`.
4. Choose a backend adapter such as `renderer/audiostream` or `renderer/ebitenaudio`.

## Atari ST Output Stage

The `github.com/jenska/ym2149/renderer/atarist` package wraps a mono source and applies a lightweight Atari ST-style board stage:

- DC blocking / AC coupling via a one-pole high-pass filter
- gentle treble roll-off via a one-pole low-pass filter
- configurable overall gain

Helpers:

- `atarist.New(source, config)`
- `(*atarist.Output).DrainMonoF32(dst)`
- `(*atarist.Output).OutputSampleRate()`

This stage is intentionally configurable because the current defaults are a practical approximation, not a finalized per-board measurement model.

## Backend-Neutral Audio Stream

The backend-neutral stereo PCM adapter lives in `renderer/audiostream`.

It is built around a minimal source interface:

```go
type MonoSource interface {
	DrainMonoF32([]float32) int
	OutputSampleRate() int
}
```

Helpers:

- `audiostream.NewReader(source, framesPerRead)` — duplicates a `MonoSource` to stereo `float32` PCM bytes
- `audiostream.NewStereoReader(source, framesPerRead)` — same, for an interleaved `StereoSource`
- `(*audiostream.Reader).Read(p []byte)` / `.Underruns()` / `.OutputSampleRate()`

This package does not import Ebiten and can be used by a future Atari ST emulator with any host audio backend that accepts an `io.Reader` or stereo `float32` PCM byte stream.

## Stereo Panning

`github.com/jenska/ym2149/renderer/stereo` turns the per-channel taps into a
stereo image. A `Panning` matrix assigns each tone channel a left/right gain;
the retro presets are `ABC()` (A left, B centre, C right — Atari ST / Amstrad
CPC), `ACB()`, and `Mono()`, with `ABCWidth(w)` / `ACBWidth(w)` for an
adjustable `0..1` stereo width.

`Splitter` mixes a channel source down to two sample-aligned mono sources, so
each side can still run through its own `bandlimited` + `atarist` chain:

```go
chip := ym2149.New(ym2149.Config{
	ClockHz:          2_000_000,
	OutputSampleRate: 48_000 * 4,
	ChannelTaps:      true,
})
// ... drive the chip ...

split := stereo.NewSplitter(chip, stereo.ABC())
left := atarist.New(mustDecimate(split.Left()), atarist.Config{})
right := atarist.New(mustDecimate(split.Right()), atarist.Config{})

out := stereo.Interleave(left, right) // audiostream.StereoSource, 48 kHz
```

`ym.Player` is also a `stereo.ChannelSource` when built with
`PlayerConfig{ChannelTaps: true}`.

## YM Music Files

The `github.com/jenska/ym2149/format/ym` package decodes and replays the Atari
ST "YM" register-dump formats and drives them through the emulation core.

- `ym.Parse(data []byte) (*Song, error)` decodes a file. `YM2!`, `YM3!`,
  `YM3b` (with loop point) and the extended `YM5!` / `YM6!` layouts are
  supported, in both interleaved and frame-major storage.
- LHArc containers are unpacked transparently: the `-lh4-`, `-lh5-` (the usual
  one), `-lh6-`, `-lh7-` and stored `-lh0-` methods are built in, so raw `.ym`
  files can be handed straight to `Parse` with no external depacker.
- `ym.NewPlayer(song, ym.PlayerConfig{...})` (or `ym.NewPlayerFromBytes`)
  returns a mono source that owns a `Chip` and advances it as it is drained,
  so it plugs directly into `renderer/bandlimited` → `renderer/atarist` → a
  backend adapter. With `PlayerConfig{ChannelTaps: true}` it is instead a
  `renderer/stereo` channel source.

```go
song, err := ym.Parse(data)
if err != nil {
	log.Fatal(err)
}
tune, err := ym.NewPlayer(song, ym.PlayerConfig{
	SampleRate: 48_000 * 4, // feed renderer/bandlimited at 4x oversampling
	Loop:       true,
})
if err != nil {
	log.Fatal(err)
}

decimator, _ := bandlimited.New(tune, bandlimited.Config{OversampleFactor: 4})
board := atarist.New(decimator, atarist.Config{})
// board is now a 48 kHz mono source playing the tune.
```

The YM5/YM6 timer effects — digidrum sample playback, the timer-synth
("SID") square-wave volume gate, and sync-buzzer envelope retriggering — are
reproduced by scheduling sub-frame register writes into the same PSG core, so
digidrums pass through the measured YM2149 output DAC like they do on real
hardware. Digidrum samples are replayed at 4-bit volume-register resolution;
the obsolete `YM4!` format and MADMAX-style `YM2!` embedded drums are not
supported.

## Ebiten Audio

The Ebiten adapter lives in `renderer/ebitenaudio`.

Helpers:

- `ebitenaudio.EnsureContext(sampleRate)`
- `ebitenaudio.NewPlayer(source, buffer)`

## Demo

The demo app is kept in `cmd/psgdemo`.

It now runs the chip at 4x the final output sample rate, then passes audio through:

`emulation -> renderer/bandlimited -> renderer/atarist -> renderer/audiostream -> renderer/ebitenaudio`

Run scripted playback:

```sh
cd cmd/psgdemo
go run . -mode script
```

Run interactive mode:

```sh
cd cmd/psgdemo
go run . -mode interactive
```

Play a YM music file (looped) through the same pipeline:

```sh
cd cmd/psgdemo
go run . -file path/to/tune.ym
```

Add `-stereo` to any mode to pan the channels A-left / B-centre / C-right
(`emulation` channel taps → `renderer/stereo` → dual `bandlimited`+`atarist`
→ `renderer/audiostream` stereo → `renderer/ebitenaudio`):

```sh
cd cmd/psgdemo
go run . -stereo -file path/to/tune.ym
```

Interactive controls:

- `Left` / `Right`: change tone period
- `Up` / `Down`: change channel volume
- `Q` / `A`: change noise period
- `T`: toggle tone
- `N`: toggle noise
- `E`: toggle envelope mode
- `[` / `]`: change envelope shape
- `Tab`: switch between scripted and interactive modes

## Testing

Run the root module test suite:

```sh
go test ./...
```

Run the standard release sanity check:

```sh
make release-check
```

The repository includes:

- register and port behavior tests
- envelope shape tests for all 16 shapes
- PCM determinism tests across different `Step` chunk sizes
- backend-neutral audio stream tests
- demo sequence smoke tests
- benchmarks for stepping, draining, and the audio pipeline
- band-limited decimator tests for DC preservation and high-frequency attenuation
- LZH depacker round-trip tests and YM2!/YM3!/YM3b/YM5!/YM6! parser + replayer tests
- timestamped-write scheduling tests and per-channel tap isolation tests
- stereo panning / splitter tests, including a chip → hard-pan integration check

`format/ym` also carries an opt-in corpus check: point `YM_CORPUS_DIR` at a
folder of real `<name>.ym` files (with `<name>.ym.ref` reference
decompressions) and run `go test ./format/ym/ -run ExternalYMCorpus`.

## Current Limitations

- YM2149F is the target; AY-specific compatibility behavior is not implemented yet.
- `format/ym` replays digidrums at 4-bit volume-register resolution and does not support the obsolete `YM4!` format, MADMAX `YM2!` embedded drums, or the `YMT`/`MIX` tracker variants.
- The core mixes to mono; `renderer/stereo` pans the per-channel taps, but each tap is that channel's contribution with the other two muted, so the three do not recombine exactly through the non-linear output stage.
- The current ST board stage is an approximation built from simple high-pass and low-pass sections, not yet a traced schematic-accurate analog model.
- The library models chip-level port behavior, not the full Atari ST MMIO map.

## License

MIT, see `LICENSE`.
