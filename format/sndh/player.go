package sndh

import (
	"fmt"
	"time"

	ym2149 "github.com/jenska/ym2149/emulation"
)

// PlayerConfig tunes a Player. The zero value is valid.
type PlayerConfig struct {
	// SampleRate is the PSG core PCM output rate in Hz. Defaults to 48000.
	// Feed a Player straight into renderer/bandlimited by setting this to
	// targetRate * oversampleFactor.
	SampleRate int

	// Subtune selects the 1-based subtune. 0 picks the file's default.
	Subtune int

	// Loop keeps playing past the subtune's known length. Subtunes without
	// a length always play forever.
	Loop bool

	// ChannelTaps makes the Player a stereo.ChannelSource: per-tone-channel
	// PCM is buffered and drained with DrainChannelF32, and DrainMonoF32
	// returns nothing. Use it to feed renderer/stereo.
	ChannelTaps bool
}

// sliceCycles is how much CPU time is emulated per render step (5 ms).
const sliceCycles = cpuClock / 200

// Player runs an SNDH driver on an emulated Atari ST and exposes the PSG
// output as a mono PCM source compatible with renderer/bandlimited and
// renderer/atarist. With PlayerConfig.ChannelTaps it is instead a per-channel
// source for renderer/stereo.
//
// Only the YM2149 is emulated: drivers that need STe DMA sound, the blitter
// or a Falcon DSP play without those voices. MFP event-count mode (Timer B
// counting scanlines, Timer A counting DMA frames) is not modelled either.
//
// A Player is not safe for concurrent use.
type Player struct {
	file    *File
	cfg     PlayerConfig
	subtune int
	chip    *ym2149.Chip
	m       *machine

	limit    uint64 // CPU cycles of playback before finishing, 0 = endless
	finished bool
	err      error

	mono    []float32    // unconsumed mono PCM (taps off)
	channel [3][]float32 // unconsumed per-channel PCM (taps on)
	drain   [512]float32
}

// NewPlayer builds a Player for an already-parsed file and runs the
// subtune's INIT routine.
func NewPlayer(f *File, cfg PlayerConfig) (*Player, error) {
	if f == nil || len(f.Data) < 16 {
		return nil, fmt.Errorf("sndh: nil or empty file")
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 48_000
	}
	subtune := cfg.Subtune
	if subtune == 0 {
		subtune = f.DefaultSubtune
	}
	if subtune < 1 || subtune > f.Subtunes {
		return nil, fmt.Errorf("sndh: subtune %d out of range 1..%d", subtune, f.Subtunes)
	}

	p := &Player{
		file:    f,
		cfg:     cfg,
		subtune: subtune,
		chip: ym2149.New(ym2149.Config{
			ClockHz:          psgClock,
			OutputSampleRate: cfg.SampleRate,
			ChannelTaps:      cfg.ChannelTaps,
		}),
	}
	m, err := newMachine(f, p.chip)
	if err != nil {
		return nil, err
	}
	p.m = m
	if err := m.boot(subtune); err != nil {
		return nil, err
	}
	if d := f.Duration(subtune); d > 0 && !cfg.Loop {
		p.limit = uint64(d.Seconds() * cpuClock)
	}
	return p, nil
}

// NewPlayerFromBytes parses raw file bytes (ICE! packed or not) and returns
// a ready Player.
func NewPlayerFromBytes(data []byte, cfg PlayerConfig) (*Player, error) {
	f, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return NewPlayer(f, cfg)
}

// File returns the parsed file being played.
func (p *Player) File() *File { return p.file }

// Subtune reports the 1-based subtune being played.
func (p *Player) Subtune() int { return p.subtune }

// Chip exposes the underlying PSG core (for inspection; do not step it directly).
func (p *Player) Chip() *ym2149.Chip { return p.chip }

// OutputSampleRate implements the mono/channel source interface.
func (p *Player) OutputSampleRate() int { return p.cfg.SampleRate }

// ChannelTapsEnabled reports whether the Player is a per-channel source.
func (p *Player) ChannelTapsEnabled() bool { return p.cfg.ChannelTaps }

// Duration reports the subtune's length from the header, or 0 when unknown.
func (p *Player) Duration() time.Duration { return p.file.Duration(p.subtune) }

// Position reports how long PLAY has been running.
func (p *Player) Position() time.Duration {
	if p.m.playStart == 0 {
		return 0
	}
	return time.Duration(float64(p.m.cpu.Cycles()-p.m.playStart) / cpuClock * float64(time.Second))
}

// Finished reports whether playback has stopped: the subtune reached its
// length (without Loop) or the driver crashed (see Err).
func (p *Player) Finished() bool { return p.finished }

// Err reports why playback stopped early, if it did.
func (p *Player) Err() error { return p.err }

// DrainMonoF32 fills dst with mono PCM, advancing the emulation as needed. It
// returns the number of samples written, which is < len(dst) only once
// playback has finished. It returns 0 when PlayerConfig.ChannelTaps is set
// (drain per channel with DrainChannelF32 instead).
func (p *Player) DrainMonoF32(dst []float32) int {
	if p.cfg.ChannelTaps {
		return 0
	}
	n := 0
	for n < len(dst) {
		if len(p.mono) == 0 {
			if p.finished {
				break
			}
			p.renderSlice()
			continue
		}
		c := copy(dst[n:], p.mono)
		p.mono = p.mono[c:]
		n += c
	}
	return n
}

// DrainChannelF32 fills dst with PCM for one tone channel (0=A, 1=B, 2=C),
// advancing the emulation as needed. It returns 0 unless
// PlayerConfig.ChannelTaps is set. Implements stereo.ChannelSource.
func (p *Player) DrainChannelF32(ch int, dst []float32) int {
	if !p.cfg.ChannelTaps || ch < 0 || ch > 2 {
		return 0
	}
	n := 0
	for n < len(dst) {
		if len(p.channel[ch]) == 0 {
			if p.finished {
				break
			}
			p.renderSlice()
			continue
		}
		c := copy(dst[n:], p.channel[ch])
		p.channel[ch] = p.channel[ch][c:]
		n += c
	}
	return n
}

// renderSlice emulates sliceCycles of machine time and collects the PCM the
// PSG produced meanwhile.
func (p *Player) renderSlice() {
	m := p.m
	if err := m.cpu.RunCycles(sliceCycles); err != nil {
		p.stop(fmt.Errorf("sndh: cpu: %w", err))
	}
	if m.crashed {
		p.stop(fmt.Errorf("sndh: driver crashed: %s", m.crash))
	}
	if p.limit != 0 && m.playStart != 0 && m.cpu.Cycles()-m.playStart >= p.limit {
		p.finished = true
	}

	target := m.psgCycle()
	const chunk = 1024
	for c := p.chip.Cycles(); c < target; c = p.chip.Cycles() {
		p.chip.Step(uint32(min(target-c, chunk)))
		if p.cfg.ChannelTaps {
			for ch := range 3 {
				for {
					got := p.chip.DrainChannelF32(ch, p.drain[:])
					p.channel[ch] = append(p.channel[ch], p.drain[:got]...)
					if got < len(p.drain) {
						break
					}
				}
			}
		} else {
			for {
				got := p.chip.DrainMonoF32(p.drain[:])
				p.mono = append(p.mono, p.drain[:got]...)
				if got < len(p.drain) {
					break
				}
			}
		}
	}
}

// Seek moves playback to position d (measured like Position). Seeking
// backwards restarts the subtune. The skipped time is emulated without
// sound, which is several times faster than rendering it, and the PSG resumes
// with the registers the driver left behind.
func (p *Player) Seek(d time.Duration) error {
	m := p.m
	if d < p.Position() || p.err != nil {
		if err := m.boot(p.subtune); err != nil {
			return err
		}
		p.err = nil
	}
	p.finished = false
	p.mono = p.mono[:0]
	for ch := range p.channel {
		p.channel[ch] = p.channel[ch][:0]
	}

	target := uint64(max(d, 0).Seconds() * cpuClock)
	// INIT time does not count towards the position, but a driver whose
	// INIT never returns must not hang the seek.
	deadline := m.cpu.Cycles() + target + 10*cpuClock
	m.skipping = true
	for m.playStart == 0 || m.cpu.Cycles()-m.playStart < target {
		if m.crashed || m.cpu.Cycles() >= deadline {
			break
		}
		if err := m.cpu.RunCycles(sliceCycles); err != nil {
			p.stop(fmt.Errorf("sndh: cpu: %w", err))
			break
		}
	}
	m.resyncPSG()
	if m.crashed {
		p.stop(fmt.Errorf("sndh: driver crashed: %s", m.crash))
	}
	if p.limit != 0 && m.playStart != 0 && m.cpu.Cycles()-m.playStart >= p.limit {
		p.finished = true
	}
	return nil
}

func (p *Player) stop(err error) {
	if p.err == nil {
		p.err = err
	}
	p.finished = true
}
