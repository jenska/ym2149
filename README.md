# YM2149 Cycle-accurate YM2149F / Atari ST PSG emulation in Go

This repository is intended to be reused later as the sound subsystem for a larger Atari ST emulator. The current focus is a reusable chip core with deterministic timing, a separate Ebiten-facing renderer/audio adapter, and a demo harness for quick listening and debugging.

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
- `renderer/bandlimited`: oversampling + FIR decimation renderer
- `renderer/ebitenaudio`: Ebiten audio reader/player helpers
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

import ym2149 "ym2149/emulation"

func main() {
	chip := ym2149.New(ym2149.Config{
		ClockHz:          2_000_000,
		OutputSampleRate: 48_000 * 4,
		BufferSamples:    4_096 * 4,
	})

	chip.SelectRegister(0)
	chip.WriteData(0x20)
	chip.SelectRegister(1)
	chip.WriteData(0x01)

	chip.SelectRegister(7)
	chip.WriteData(0x3e)
	chip.SelectRegister(8)
	chip.WriteData(0x0f)

chip.Step(20_000)

	decimator, _ := bandlimited.New(chip, bandlimited.Config{
		OversampleFactor: 4,
	})
	board := atarist.New(decimator, atarist.Config{})
	samples := make([]float32, chip.BufferedSamples())
	n := board.DrainMonoF32(samples)
	_ = samples[:n]
}
```

## Core API

The `ym2149/emulation` package currently exposes:

- `New(Config) *Chip`
- `(*Chip).Reset()`
- `(*Chip).Step(cycles uint32)`
- `(*Chip).Cycles() uint64`
- `(*Chip).ClockHz() int`
- `(*Chip).OutputSampleRate() int`
- `(*Chip).BufferedSamples() int`
- `(*Chip).SelectRegister(reg byte)`
- `(*Chip).WriteData(v byte)`
- `(*Chip).ReadData() byte`
- `(*Chip).SetPortAInput(v byte)`
- `(*Chip).SetPortBInput(v byte)`
- `(*Chip).Ports() Ports`
- `(*Chip).DrainMonoF32(dst []float32) int`

The library is safe to call from concurrent goroutines, which keeps it practical for a future emulator thread driving chip state while an audio thread drains samples.

## Band-Limited Renderer

The `renderer/bandlimited` package downsamples an oversampled mono source through a windowed-sinc FIR:

- `bandlimited.New(source, config)`
- `(*bandlimited.Decimator).DrainMonoF32(dst)`
- `(*bandlimited.Decimator).OutputSampleRate()`

Typical usage is:

1. Configure the chip to produce PCM at `targetSampleRate * oversampleFactor`.
2. Wrap it with `renderer/bandlimited`.
3. Pass the decimated output into `renderer/atarist` and then `renderer/ebitenaudio`.

## Atari ST Output Stage

The `renderer/atarist` package wraps a mono source and applies a lightweight Atari ST-style board stage:

- DC blocking / AC coupling via a one-pole high-pass filter
- gentle treble roll-off via a one-pole low-pass filter
- configurable overall gain

Helpers:

- `atarist.New(source, config)`
- `(*atarist.Output).DrainMonoF32(dst)`
- `(*atarist.Output).OutputSampleRate()`

This stage is intentionally configurable because the current defaults are a practical approximation, not a finalized per-board measurement model.

## Ebiten Audio

The Ebiten adapter lives in `renderer/ebitenaudio` and is built around a minimal source interface:

```go
type MonoSource interface {
	DrainMonoF32([]float32) int
	OutputSampleRate() int
}
```

Helpers:

- `ebitenaudio.NewReader(source, framesPerRead)`
- `ebitenaudio.EnsureContext(sampleRate)`
- `ebitenaudio.NewPlayer(source, buffer)`

## Demo

The demo app is kept in `cmd/psgdemo`.

It now runs the chip at 4x the final output sample rate, then passes audio through:

`emulation -> renderer/bandlimited -> renderer/atarist -> renderer/ebitenaudio`

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

Run the root test suite:

```sh
go test ./...
```

The repository includes:

- register and port behavior tests
- envelope shape tests for all 16 shapes
- PCM determinism tests across different `Step` chunk sizes
- Ebiten adapter tests
- demo sequence smoke tests
- benchmarks for stepping, draining, and the audio pipeline
- band-limited decimator tests for DC preservation and high-frequency attenuation

## Current Limitations

- YM2149F is the target; AY-specific compatibility behavior is not implemented yet.
- The output path is mono-at-the-core and duplicated to stereo in the Ebiten adapter.
- The current ST board stage is an approximation built from simple high-pass and low-pass sections, not yet a traced schematic-accurate analog model.
- The library models chip-level port behavior, not the full Atari ST MMIO map.

## License

MIT, see `LICENSE`.
