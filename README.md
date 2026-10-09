# YM2149

Cycle-accurate YM2149F / Atari ST PSG emulation in Go — plus
[**`sndplayer`**](#-sndplayer--atari-st-music-in-your-terminal), a terminal
player for Atari ST SNDH and YM music.

Latest release: `v1.3.0`

This repository is intended to be reused later as the sound subsystem for a larger Atari ST emulator. The current focus is a reusable chip core with deterministic timing, backend-neutral audio rendering helpers, an Ebiten adapter, YM and SNDH music-file players, the `sndplayer` terminal music player, and a demo harness for quick listening and debugging.

## 🎵 sndplayer — Atari ST music in your terminal

**`sndplayer` is a ready-to-use music player built on this library.** It plays
**SNDH** files (the original 68000 music drivers, running on an emulated Atari
ST) and **YM** register dumps, right in your terminal — with per-voice
oscilloscopes, a seekable time bar, playlists and WAV export. Point it at a
single tune, a folder or the whole [SNDH archive](https://sndh.atari.org) ZIP.

```sh
go install github.com/jenska/ym2149/cmd/sndplayer@latest
sndplayer sndh_lf.zip
```

Works on macOS, Linux and Windows terminals with Unicode and mouse support.
On Linux, audio output needs the ALSA headers to build (`libasound2-dev` on
Debian/Ubuntu, `alsa-lib-devel` on Fedora).

```text
 ♫ sndplayer · SNDH & YM player · YM2149 + 68000     ▶ playing · mode Continuous · stereo ABC
╭─ Song ───────────────────────────────────────────────────────────────────────────────────╮
│ Title     Bug Bash                              Year    1991                             │
│ Composer  Rob Brooks                            Subtune 3 of 6                           │
│ Ripper    Grazey/PHF                            Replay  Timer C at 50 Hz                 │
╰──────────────────────────────────────────────────────────────────────────────────────────╯
╭─ Voice A ───────────────────╮╭─ Voice B ───────────────────╮╭─ Voice C ──────────────────╮
│                             ││⡏⠉⠉⠉⢹    ⡏⠉⠉⠉⠉⡇   ⢸⠉⠉⠉⠉⡇     ││                            │
│⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤││⡇   ⢸    ⡇    ⡇   ⢸    ⡇    ││⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤⠤│
│                             ││⠃   ⠘⠒⠒⠒⠒⠃    ⠓⠒⠒⠒⠚    ⠓⠒⠒  ││                            │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░ ││ █████░░░░░░░░░░░░░░░░░░░░░░ ││ ░░░░░░░░░░░░░░░░░░░░░░░░░░ │
╰─────────────────────────────╯╰─────────────────────────────╯╰────────────────────────────╯
 ▶ 00:01 ━●──────────────────────────────────────────────────────────────────────── 01:10
╭─ Files 2/3 ──────────────────────────────────────────╮╭─ Subtunes 3/6 ────────────────────╮
│  aerius.snd · Aerius                                 ││   1 *                       01:48 │
│♪ bugbash.snd · Bug Bash                              ││   2                         00:39 │
│  doodbug.snd · Doodle Bug                            ││♪  3                         01:10 │
╰──────────────────────────────────────────────────────╯╰───────────────────────────────────╯
```

Inspired by Arnaud Carré's [SNDH-Player](https://github.com/arnaud-carre/sndh-player):

- **Formats:** `.sndh`/`.snd` (plain or ICE! packed) and `.ym` (YM2!–YM6!,
  plain or LHA packed), recognised by content.
- **Playlists:** files, folders (searched recursively) and ZIP archives with
  thousands of tunes; tunes are only loaded when shown.
- **Display:** song details, braille oscilloscope and level meter per voice,
  file list and subtune list with names and lengths.
- **Playback:** instant seeking (keys or mouse click on the time bar), play
  modes Single / Loop / Continuous / Random, ABC / ACB / mono stereo.
- **WAV export** of the playing tune (`w`), or headless with `-wav`.
- **Accurate sound:** the same cycle-accurate YM2149 core, band-limited
  resampling and Atari ST output filter as the rest of this library.

More examples:

```sh
sndplayer "Rob Hubbard - Goldrunner.sndh"            # a single tune
sndplayer -mode loop -subtune 3 tune.sndh            # loop subtune 3
sndplayer ~/music/*.ym                               # YM files
sndplayer -wav out.wav -subtune 2 tune.sndh          # render to WAV and exit
```

| Key | Action |
| --- | --- |
| `space` | pause / resume (restart a finished tune) |
| `←` `→` / `<` `>` | seek 5 s / 30 s; `0`–`9` jump to 0–90 % |
| `tab`, `↑` `↓` `PgUp` `PgDn` `Home` `End` | move between and within the file and subtune lists |
| `enter` | play the selected file or subtune |
| `n` `p` / `N` `P` | next / previous subtune / file; `r` random tune |
| `m` | play mode: Single, Loop, Continuous (next subtune, then next file), Random |
| `s` | stereo: ABC, ACB, mono |
| `w` | export the playing subtune to `<file>-<subtune>.wav` |
| `q` | quit |

The mouse works too: click the time bar to seek, click a list row to select
it and again to play it, and scroll the lists with the wheel. Subtunes without
a length in the header count as `-length` long (default 3 minutes) in the
Single, Continuous and Random modes. A YM file is a single subtune; its song
panel shows the YM name, author, comment, frame rate, PSG clock, loop point and
digidrum count. Set `NO_COLOR` for a monochrome UI. Run `sndplayer -h` for all
flags.

## Versioning

- The root module path is `github.com/jenska/ym2149`.
- This repository uses a single Go module. All packages under `emulation/`, `renderer/`, `format/`, `internal/`, and `cmd/` are released together under the root module tag.
- Requires Go 1.27 or newer.

## Installation

Library:

```sh
go get github.com/jenska/ym2149@v1.3.0
```

Music player:

```sh
go install github.com/jenska/ym2149/cmd/sndplayer@latest
```

## Status

- YM2149F-oriented core with all 16 PSG registers
- Clock-driven stepping API for emulator integration
- Tone, noise, envelope, mixer, and I/O port handling
- YM2149-style nonlinear analog mix table for mono PCM generation
- Ebiten audio adapter that exposes stereo `float32` PCM to `audio.NewPlayerF32`
- SNDH (68000 drivers on an emulated Atari ST) and YM music-file players
- `sndplayer`: terminal music player with oscilloscopes, playlists and WAV export
- Scripted and interactive demo scaffolding

## Package Layout

- `emulation`: reusable PSG core package
- `renderer/atarist`: Atari ST board-output approximation
- `renderer/audiostream`: backend-neutral mono→stereo and interleaved-stereo PCM readers
- `renderer/bandlimited`: oversampling + FIR decimation renderer
- `renderer/stereo`: per-channel panning (ABC / ACB / mono) into a stereo pair
- `renderer/ebitenaudio`: Ebiten audio reader/player helpers
- `format/ym`: YM2!/YM3!/YM3b/YM5!/YM6! music-file decoder and replayer
- `format/sndh`: SNDH music-file player (68000 driver on an emulated minimal Atari ST)
- `internal/psgdemo`: shared scripted demo logic
- `cmd/psgdemo`: Ebiten demo app
- `cmd/sndplayer`: terminal SNDH and YM player with oscilloscopes

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
  `renderer/stereo` channel source. `Position`, `Duration` and `Seek` report
  and move the playing time; with `Loop`, positions past the end land inside
  the loop.

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

## SNDH Music Files

The `github.com/jenska/ym2149/format/sndh` package plays
[SNDH](https://sndh.atari.org) files. An SNDH file holds the original 68000
music driver, so playing one means running real Atari ST code: the package
boots a minimal ST around the [`m68kemu`](https://github.com/jenska/m68kemu)
CPU core and feeds the PSG writes it makes into the emulation core with
cycle-exact timestamps (the CPU runs at exactly 4x the PSG clock).

- `sndh.Parse(data []byte) (*File, error)` reads the header: title, composer,
  ripper, converter, year, subtune count, default subtune, subtune names
  (`#!SN`), replay timer and rate (`TA`/`TB`/`TC`/`TD`/`!V`), lengths
  (`FRMS`, or the older `TIME`) and `FLAG`. ICE! 2.4 packed files (most of
  the archive) are depacked transparently; `sndh.DepackICE` is exported too.
- `sndh.NewPlayer(file, sndh.PlayerConfig{...})` (or `sndh.NewPlayerFromBytes`)
  runs the subtune's INIT routine and returns a source with the same shape as
  `ym.Player`: mono by default, a `renderer/stereo` channel source with
  `ChannelTaps`. `Subtune` selects the subtune (1-based, 0 = default). Without
  `Loop`, playback stops at the length given in the header; tunes without a
  length play forever.

```go
tune, err := sndh.NewPlayerFromBytes(data, sndh.PlayerConfig{
	SampleRate: 48_000 * 4, // feed renderer/bandlimited at 4x oversampling
	Subtune:    0,          // the file's default subtune
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(tune.File().Title, tune.Duration())

decimator, _ := bandlimited.New(tune, bandlimited.Config{OversampleFactor: 4})
board := atarist.New(decimator, atarist.Config{})
```

The emulated machine is a PAL ST with 4 MB of RAM:

- MC68901 MFP: all four timers in delay mode, interrupt enable/pending/
  in-service/mask registers, software and automatic end-of-interrupt, on
  interrupt level 6. Drivers that use Timer A/B/D for SID voices, digidrums or
  sync-buzzer effects get real timer interrupts.
- VBL on level 4 at 50.05 Hz, including the TOS VBL queue.
- A small TOS stand-in ([`format/sndh/tos.asm`](format/sndh/tos.asm),
  assembled at startup with [`m68kasm`](https://github.com/jenska/m68kasm)):
  the OS header, system variables, the 200 Hz Timer C tick with `etv_timer`,
  and the GEMDOS/BIOS/XBIOS calls drivers actually use (`Super`, `Malloc`,
  `Setexc`, `Xbtimer`, `Jenabint`/`Jdisint`, `Mfpint`, `Giaccess`,
  `Ongibit`/`Offgibit`, `Supexec`, `Kbdvbase`, ...).
- PLAY is called the way SND Player does it: `TC` rates that divide 200 Hz
  are derived from the TOS Timer C tick, other `TC` rates reprogram Timer C,
  `TA`/`TB`/`TD` program that timer, and `!V` plays from the VBL.

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

SNDH files work the same way; `-subtune n` picks a subtune:

```sh
cd cmd/psgdemo
go run . -file path/to/tune.sndh -subtune 2
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

- ICE! depacker round trips (against a test-only packer), SNDH header parsing,
  MFP timer/interrupt tests, and SNDH replay tests that assemble small 68000
  drivers to check replay rates, timer interrupts, TOS calls, subtunes,
  lengths and crash handling

`format/sndh` has an opt-in corpus check too: point `SNDH_CORPUS_DIR` at a
folder of `.sndh`/`.snd` files and run
`go test ./format/sndh/ -run ExternalSNDHCorpus -v`.

`format/ym` also carries an opt-in corpus check: point `YM_CORPUS_DIR` at a
folder of real `<name>.ym` files (with `<name>.ym.ref` reference
decompressions) and run `go test ./format/ym/ -run ExternalYMCorpus`.

## Current Limitations

- YM2149F is the target; AY-specific compatibility behavior is not implemented yet.
- `format/ym` replays digidrums at 4-bit volume-register resolution and does not support the obsolete `YM4!` format, MADMAX `YM2!` embedded drums, or the `YMT`/`MIX` tracker variants.
- `format/sndh` emulates only the YM2149 side of the machine: STe/Falcon DMA sound, the blitter, the DSP and MFP event-count mode (Timer B counting scanlines, Timer A counting DMA frames) are not emulated, so tunes that rely on them play without those parts. The machine is a plain ST with no cookie-jar entries beyond `_MCH` = ST and `_SND` = PSG.
- The core mixes to mono; `renderer/stereo` pans the per-channel taps, but each tap is that channel's contribution with the other two muted, so the three do not recombine exactly through the non-linear output stage.
- The current ST board stage is an approximation built from simple high-pass and low-pass sections, not yet a traced schematic-accurate analog model.
- The library models chip-level port behavior, not the full Atari ST MMIO map.

## License

MIT, see `LICENSE`.
