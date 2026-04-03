# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

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
