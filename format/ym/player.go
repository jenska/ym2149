package ym

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	ym2149 "github.com/jenska/ym2149/emulation"
)

// PlayerConfig tunes a Player. The zero value is valid.
type PlayerConfig struct {
	// SampleRate is the PSG core PCM output rate in Hz. Defaults to 48000.
	// Feed a Player straight into renderer/bandlimited by setting this to
	// targetRate * oversampleFactor.
	SampleRate int

	// ClockHz overrides the master clock. Defaults to the value stored in the
	// file (2 MHz for YM2!/YM3!).
	ClockHz int

	// Loop restarts from Song.LoopFrame after the final frame instead of
	// going silent.
	Loop bool

	// ChannelTaps makes the Player a stereo.ChannelSource: per-tone-channel
	// PCM is buffered and drained with DrainChannelF32, and DrainMonoF32
	// returns nothing. Use it to feed renderer/stereo.
	ChannelTaps bool
}

// Player replays a Song through a cycle-accurate Chip and exposes the result as
// a mono PCM source compatible with renderer/bandlimited and renderer/atarist.
// With PlayerConfig.ChannelTaps it is instead a per-channel source for
// renderer/stereo.
//
// A Player is not safe for concurrent use.
type Player struct {
	song *Song
	chip *ym2149.Chip
	cfg  PlayerConfig

	clockHz        int
	sampleRate     int
	frameHz        int
	cyclesPerFrame uint32
	taps           bool

	frame    int
	played   int // frames rendered since the start, across loops
	finished bool

	mono    []float32    // unconsumed mono PCM (taps off)
	channel [3][]float32 // unconsumed per-channel PCM (taps on)
	drain   [512]float32

	evBuf  []regEvent
	drums  [3]drumVoice
	sids   [3]sidVoice
	buzzer buzzerVoice
}

type regEvent struct {
	at  uint32
	reg byte
	val byte
}

type drumVoice struct {
	active bool
	data   []byte
	freq   int
	idx    int
	acc    uint32 // cycles already elapsed toward the next sample at frame start
}

type sidVoice struct {
	active bool
	acc    uint32
	high   bool
}

type buzzerVoice struct {
	active bool
	acc    uint32
}

// NewPlayer builds a Player for an already-decoded Song.
func NewPlayer(song *Song, cfg PlayerConfig) (*Player, error) {
	if song == nil || len(song.Frames) == 0 {
		return nil, fmt.Errorf("ym: nil or empty song")
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 48_000
	}
	clock := cfg.ClockHz
	if clock <= 0 {
		clock = song.ClockHz
	}
	if clock <= 0 {
		clock = atariSTClock
	}
	frameHz := song.FrameHz
	if frameHz <= 0 {
		frameHz = 50
	}

	p := &Player{
		song:           song,
		cfg:            cfg,
		clockHz:        clock,
		sampleRate:     cfg.SampleRate,
		frameHz:        frameHz,
		cyclesPerFrame: uint32(clock / frameHz),
		taps:           cfg.ChannelTaps,
	}
	p.chip = ym2149.New(ym2149.Config{
		ClockHz:          clock,
		OutputSampleRate: cfg.SampleRate,
		ChannelTaps:      cfg.ChannelTaps,
	})
	return p, nil
}

// NewPlayerFromBytes parses raw file bytes (compressed or not) and returns a
// ready Player.
func NewPlayerFromBytes(data []byte, cfg PlayerConfig) (*Player, error) {
	song, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return NewPlayer(song, cfg)
}

// Song returns the decoded song being played.
func (p *Player) Song() *Song { return p.song }

// Chip exposes the underlying PSG core (for inspection; do not step it directly).
func (p *Player) Chip() *ym2149.Chip { return p.chip }

// OutputSampleRate implements the mono/channel source interface.
func (p *Player) OutputSampleRate() int { return p.sampleRate }

// ChannelTapsEnabled reports whether the Player is a per-channel source.
func (p *Player) ChannelTapsEnabled() bool { return p.taps }

// Frame reports the next frame index to be played.
func (p *Player) Frame() int { return p.frame }

// TotalFrames reports the number of frames in the song.
func (p *Player) TotalFrames() int { return len(p.song.Frames) }

// Finished reports whether a non-looping song has played its last frame.
func (p *Player) Finished() bool { return p.finished }

// Duration reports the un-looped length of the song.
func (p *Player) Duration() time.Duration {
	return time.Duration(len(p.song.Frames)) * time.Second / time.Duration(p.frameHz)
}

// Position reports the playing time so far. It keeps counting across loops.
func (p *Player) Position() time.Duration {
	return time.Duration(p.played) * time.Second / time.Duration(p.frameHz)
}

// Seek moves playback to position d, measured like Position: with Loop, a
// position past the end lands inside the loop. Effects restart and the last
// envelope shape written before the new frame is restored.
func (p *Player) Seek(d time.Duration) {
	n := len(p.song.Frames)
	target := int(max(d, 0) * time.Duration(p.frameHz) / time.Second)
	p.played = target
	p.finished = false
	switch {
	case target < n:
		p.frame = target
	case p.cfg.Loop:
		p.frame = p.song.LoopFrame + (target-n)%(n-p.song.LoopFrame)
	default:
		p.frame, p.played, p.finished = n, n, true
	}
	p.resetEffects()
	p.mono = p.mono[:0]
	for ch := range p.channel {
		p.channel[ch] = p.channel[ch][:0]
	}
	if p.song.Version >= 3 {
		for f := min(p.frame, n) - 1; f >= 0; f-- {
			if r13 := p.song.Frames[f][13]; r13 != 0xff {
				p.chip.SelectRegister(13)
				p.chip.WriteData(r13 & 0x0f)
				break
			}
		}
	}
}

// DrainMonoF32 fills dst with mono PCM, advancing the song as needed. It returns
// the number of samples written, which is < len(dst) only once a non-looping
// song is exhausted. It returns 0 when PlayerConfig.ChannelTaps is set (drain
// per channel with DrainChannelF32 instead).
func (p *Player) DrainMonoF32(dst []float32) int {
	if p.taps {
		return 0
	}
	n := 0
	for n < len(dst) {
		if len(p.mono) == 0 {
			if p.finished {
				break
			}
			p.renderFrame()
			if len(p.mono) == 0 {
				break
			}
		}
		c := copy(dst[n:], p.mono)
		p.mono = p.mono[c:]
		n += c
	}
	return n
}

// DrainChannelF32 fills dst with PCM for one tone channel (0=A, 1=B, 2=C),
// advancing the song as needed. It returns 0 unless PlayerConfig.ChannelTaps
// is set. Implements stereo.ChannelSource.
func (p *Player) DrainChannelF32(ch int, dst []float32) int {
	if !p.taps || ch < 0 || ch > 2 {
		return 0
	}
	n := 0
	for n < len(dst) {
		if len(p.channel[ch]) == 0 {
			if p.finished {
				break
			}
			p.renderFrame()
			if len(p.channel[ch]) == 0 {
				break
			}
		}
		c := copy(dst[n:], p.channel[ch])
		p.channel[ch] = p.channel[ch][c:]
		n += c
	}
	return n
}

func (p *Player) renderFrame() {
	if p.frame >= len(p.song.Frames) {
		if !p.cfg.Loop {
			p.finished = true
			return
		}
		p.frame = p.song.LoopFrame
		p.resetEffects()
	}

	events := p.buildEvents(p.song.Frames[p.frame])

	var cur uint32
	for _, e := range events {
		if e.at > cur {
			p.stepDrain(e.at - cur)
			cur = e.at
		}
		p.chip.SelectRegister(e.reg)
		p.chip.WriteData(e.val)
	}
	if cur < p.cyclesPerFrame {
		p.stepDrain(p.cyclesPerFrame - cur)
	}
	p.frame++
	p.played++
}

func (p *Player) stepDrain(cycles uint32) {
	const chunk = 1024
	for cycles > 0 {
		n := min(cycles, chunk)
		p.chip.Step(n)
		if p.taps {
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
		cycles -= n
	}
}

func (p *Player) resetEffects() {
	p.drums = [3]drumVoice{}
	p.sids = [3]sidVoice{}
	p.buzzer = buzzerVoice{}
}

// buildEvents produces the ordered register writes for one frame: the base
// R0..R13 snapshot at cycle 0, followed by any sub-frame writes needed to
// reproduce the active YM5/YM6 timer effects.
func (p *Player) buildEvents(fb [16]byte) []regEvent {
	ev := p.evBuf[:0]
	cpf := p.cyclesPerFrame

	var req effectReq
	if p.song.Version >= 5 {
		req = decodeEffects(p.song.Version, fb)
	}

	// (Re)start digidrums requested this frame.
	for v := range 3 {
		if !req.drum[v].on || req.drum[v].freq <= 0 {
			continue
		}
		n := int(fb[8+v]) & 0x1f
		if n >= len(p.song.Drums) || len(p.song.Drums[n]) == 0 {
			continue
		}
		p.drums[v] = drumVoice{active: true, data: p.song.Drums[n], freq: req.drum[v].freq}
	}

	// Effective mixer: force tone+noise off on any voice with a running drum.
	r7 := fb[7]
	for v := range 3 {
		if p.drums[v].active {
			r7 |= 1<<uint(v) | 1<<uint(v+3)
		}
	}

	for r := 0; r <= 10; r++ {
		val := fb[r]
		if r == 7 {
			val = r7
		}
		ev = append(ev, regEvent{0, byte(r), val})
	}
	if p.song.Version >= 3 {
		ev = append(ev, regEvent{0, 11, fb[11]}, regEvent{0, 12, fb[12]})
		if fb[13] != 0xff {
			ev = append(ev, regEvent{0, 13, fb[13] & 0x0f})
		}
	}

	ev = p.scheduleDrums(ev, fb, cpf)
	ev = p.scheduleSids(ev, req, fb, cpf)
	ev = p.scheduleBuzzer(ev, req, cpf)

	slices.SortStableFunc(ev, func(a, b regEvent) int { return cmp.Compare(a.at, b.at) })
	p.evBuf = ev
	return ev
}

// minEffectPeriod clamps pathological timer rates (a few master cycles) to one
// internal PSG tick; anything faster cannot be represented and is inaudible.
const minEffectPeriod = 8

func (p *Player) scheduleDrums(ev []regEvent, fb [16]byte, cpf uint32) []regEvent {
	for v := range 3 {
		dv := &p.drums[v]
		if !dv.active {
			continue
		}
		period := max(uint32(p.clockHz)/uint32(dv.freq), minEffectPeriod)
		at := dv.acc
		for at < cpf {
			if dv.idx >= len(dv.data) {
				dv.active = false
				// Hand the voice back to normal tone/noise for the rest of the frame.
				ev = append(ev,
					regEvent{at, byte(8 + v), fb[8+v] & 0x1f},
					regEvent{at, 7, p.mixerAfterDrum(fb[7], v)},
				)
				break
			}
			ev = append(ev, regEvent{at, byte(8 + v), drumNibble(p.song, dv.data[dv.idx])})
			dv.idx++
			at += period
		}
		if dv.active {
			dv.acc = at - cpf
		} else {
			dv.acc = 0
		}
	}
	return ev
}

// mixerAfterDrum is fb[7] with tone+noise still forced off for any *other* drum
// that is still running.
func (p *Player) mixerAfterDrum(base byte, ended int) byte {
	r7 := base
	for v := range 3 {
		if v != ended && p.drums[v].active {
			r7 |= 1<<uint(v) | 1<<uint(v+3)
		}
	}
	return r7
}

func (p *Player) scheduleSids(ev []regEvent, req effectReq, fb [16]byte, cpf uint32) []regEvent {
	for v := range 3 {
		sv := &p.sids[v]
		if !req.sid[v].on || req.sid[v].freq <= 0 {
			sv.active = false
			continue
		}
		if !sv.active {
			sv.acc = 0
			sv.high = true
		}
		sv.active = true
		half := max(uint32(p.clockHz)/uint32(2*req.sid[v].freq), minEffectPeriod)
		vol := fb[8+v] & 0x0f
		at := sv.acc
		for at < cpf {
			val := byte(0)
			if sv.high {
				val = vol
			}
			ev = append(ev, regEvent{at, byte(8 + v), val})
			sv.high = !sv.high
			at += half
		}
		sv.acc = at - cpf
	}
	return ev
}

func (p *Player) scheduleBuzzer(ev []regEvent, req effectReq, cpf uint32) []regEvent {
	bv := &p.buzzer
	if !req.buzzer.on || req.buzzer.freq <= 0 {
		bv.active = false
		return ev
	}
	if !bv.active {
		bv.acc = 0
	}
	bv.active = true
	period := max(uint32(p.clockHz)/uint32(req.buzzer.freq), minEffectPeriod)
	shape := req.buzzer.shape & 0x0f
	at := bv.acc
	for at < cpf {
		ev = append(ev, regEvent{at, 13, shape})
		at += period
	}
	bv.acc = at - cpf
	return ev
}

func drumNibble(s *Song, sample byte) byte {
	if s.drum4bit {
		return sample & 0x0f
	}
	return sample >> 4
}
