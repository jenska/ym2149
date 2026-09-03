# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [Unreleased]

### Added

- `format/ym`: decoder and replayer for the Atari ST YM register-dump music
  formats (`YM2!`, `YM3!`, `YM3b`, `YM5!`, `YM6!`), including a built-in LHArc
  (`-lh4-`/`-lh5-`/`-lh6-`/`-lh7-`/`-lh0-`) depacker so raw `.ym` files load
  without external tooling.
- `ym.Player`: a mono source that drives the emulation core from a decoded
  song and reproduces the YM5/YM6 timer effects (digidrum, timer-synth "SID"
  voice, sync-buzzer) via scheduled sub-frame register writes.
- `emulation`: `Chip.WriteAt` schedules register writes at an absolute
  master-clock cycle, applied mid-`Step`; `Chip.Write` is an immediate direct
  write; `PendingWrites` / `ClearPendingWrites` manage the queue.
- `emulation`: `Config.ChannelTaps` buffers each tone channel's isolated PCM,
  drained with `Chip.DrainChannelF32`.
- `renderer/stereo`: `Panning` matrices (`ABC` / `ACB` / `Mono`, plus width
  variants), a `Splitter` that turns a channel source into a sample-aligned
  left/right mono pair, and `Interleave` for an interleaved stereo source.
- `renderer/audiostream`: `StereoSource` + `NewStereoReader` for interleaved
  stereo input; `ebitenaudio.NewStereoPlayer`.
- `cmd/psgdemo`: `-file <tune.ym>` plays a YM file; `-stereo` pans the channels
  A-left / B-centre / C-right.

### Changed

- Require Go 1.27; modernized loops and min/max helpers across the tree.
- Dropped the unused `renderer/ebitenaudio` / `renderer/audiostream` module
  `replace` directives.
- Updated Ebiten to v2.9.11 (oto v3.4.1).

## [1.0.0] - 2026-04-03

### Added

- Cycle-accurate YM2149F-focused emulation core with full 16-register model.
- Deterministic clock-driven stepping and PCM generation.
- Host clock-domain helpers for converting CPU or bus cycles into PSG cycles.
- Bus-timestamped regression coverage for exact write timing and envelope/noise corner cases.
- YM2149-style nonlinear analog mix model.
- Band-limited renderer with oversampling and FIR decimation.
- Atari ST-style post-chip board output approximation.
- Backend-neutral stereo PCM reader for non-Ebiten audio backends.
- Ebiten audio adapter and demo application.
- Benchmarks and regression tests covering timing, rendering, and audio conversion.

### Notes

- This is the first planned stable release of the root module `github.com/jenska/ym2149`.
- The project is YM2149F / Atari ST oriented. AY-family compatibility and fully measured board-level analog reproduction remain future work.
