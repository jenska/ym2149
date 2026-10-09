# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [Unreleased]

### Added

- `ym.Player`: `Seek`, `Position` (keeps counting across loops) and
  `Duration`. Seeking restarts the YM5/YM6 effects and restores the last
  envelope shape written before the new frame.
- `cmd/sndplayer`: plays YM files (`.ym`, LHA-packed or raw) alongside SNDH,
  in playlists, folders and ZIP archives; the format is detected by content.
  Seeking, play modes, oscilloscopes and WAV export work for both formats.

## [1.2.0] - 2026-10-04

### Added

- `format/sndh`: player for SNDH files (Atari ST music drivers). The 68000
  driver runs on `github.com/jenska/m68kemu` inside a minimal PAL Atari ST:
  4 MB RAM, the PSG with cycle-timestamped register writes, an MC68901 MFP
  (timers A-D, interrupt controller, software/auto EOI), the VBL interrupt
  and a small TOS stand-in (system variables, Timer C tick, GEMDOS/BIOS/XBIOS
  calls) written in 68000 assembly and assembled at startup with
  `github.com/jenska/m68kasm`.
- `sndh.Parse` reads all SNDH v2.2 header tags (`TITL`, `COMM`, `RIPP`,
  `CONV`, `YEAR`, `##`, `#!`, `#!SN`, `TA`/`TB`/`TC`/`TD`/`!V`, `FRMS`,
  `TIME`, `FLAG`, `HDNS`); ICE! 2.4 packed files are depacked transparently
  (`sndh.DepackICE`).
- `sndh.Player`: mono or per-channel source like `ym.Player`, with subtune
  selection, header-length based stopping and crash reporting.
- `cmd/psgdemo`: `-file` also plays `.sndh` files; `-subtune n` selects the
  subtune.
- `sndh.Player.Seek`: jump to a position; the skipped time is emulated
  without sound (about 70x realtime) and the PSG resumes with the driver's
  registers.
- `cmd/sndplayer`: terminal SNDH player modelled on SNDH-Player: song
  details, per-voice braille oscilloscopes and level meters, seekable time
  bar (keyboard and mouse), file and subtune lists, Single/Loop/Continuous/
  Random play modes, ABC/ACB/mono stereo, ZIP archive and directory
  playlists, and WAV export (`w` key or `-wav`).

## [1.1.0] - 2026-09-03

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
