package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/jenska/ym2149/format/sndh"
	"github.com/jenska/ym2149/renderer/atarist"
	"github.com/jenska/ym2149/renderer/audiostream"
	"github.com/jenska/ym2149/renderer/bandlimited"
	"github.com/jenska/ym2149/renderer/stereo"
)

const oversample = 4

type playMode int

const (
	modeSingle playMode = iota
	modeLoop
	modeContinuous
	modeRandom
	numModes
)

func (m playMode) String() string {
	return [...]string{"Single", "Loop", "Continuous", "Random"}[m]
}

func parseMode(s string) (playMode, error) {
	for m := range numModes {
		if equalFold(m.String(), s) {
			return m, nil
		}
	}
	return 0, fmt.Errorf("unknown play mode %q (single, loop, continuous, random)", s)
}

type panMode int

const (
	panABC panMode = iota
	panACB
	panMono
	numPans
)

func (p panMode) String() string { return [...]string{"ABC", "ACB", "Mono"}[p] }

func (p panMode) panning() stereo.Panning {
	switch p {
	case panACB:
		return stereo.ACBWidth(0.6)
	case panMono:
		return stereo.Mono()
	}
	return stereo.ABCWidth(0.6)
}

func parsePan(s string) (panMode, error) {
	for p := range numPans {
		if equalFold(p.String(), s) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("unknown stereo mode %q (abc, acb, mono)", s)
}

// buildChain turns a per-channel source running at rate*oversample into a
// stereo source at rate: panning, then band-limited decimation and the ST
// output filter on each side.
func buildChain(src stereo.ChannelSource, pan stereo.Panning) (*stereo.Splitter, audiostream.StereoSource, error) {
	split := stereo.NewSplitter(src, pan)
	side := func(s stereo.MonoSource) (audiostream.MonoSource, error) {
		dec, err := bandlimited.New(s, bandlimited.Config{OversampleFactor: oversample})
		if err != nil {
			return nil, err
		}
		return atarist.New(dec, atarist.Config{}), nil
	}
	left, err := side(split.Left())
	if err != nil {
		return nil, nil, err
	}
	right, err := side(split.Right())
	if err != nil {
		return nil, nil, err
	}
	return split, stereo.Interleave(left, right), nil
}

func newTunePlayer(f *sndh.File, subtune, rate int) (*sndh.Player, error) {
	return sndh.NewPlayer(f, sndh.PlayerConfig{
		SampleRate:  rate * oversample,
		Subtune:     subtune,
		Loop:        true, // the UI decides when a tune ends
		ChannelTaps: true,
	})
}

const scopeLen = 4096 // per-voice history at the output rate

// tap records every voice's PCM, decimated to the output rate, for the
// oscilloscopes.
type tap struct {
	src   *sndh.Player
	ring  [3][scopeLen]float32
	pos   [3]int
	phase [3]int
}

func (t *tap) OutputSampleRate() int { return t.src.OutputSampleRate() }

func (t *tap) DrainChannelF32(ch int, dst []float32) int {
	n := t.src.DrainChannelF32(ch, dst)
	for _, v := range dst[:n] {
		if t.phase[ch]++; t.phase[ch] == oversample {
			t.phase[ch] = 0
			t.ring[ch][t.pos[ch]] = v
			t.pos[ch] = (t.pos[ch] + 1) % scopeLen
		}
	}
	return n
}

// engine owns the playing tune and feeds the audio device. The audio callback
// (Read) and the UI share it under mu.
type engine struct {
	mu   sync.Mutex
	rate int

	mode          playMode
	pan           panMode
	defaultLength time.Duration

	player *sndh.Player
	tap    *tap
	split  *stereo.Splitter
	reader *audiostream.StereoReader

	paused bool
	ended  bool
	endCh  chan struct{}
}

func newEngine(rate int, mode playMode, pan panMode, defaultLength time.Duration) *engine {
	return &engine{rate: rate, mode: mode, pan: pan, defaultLength: defaultLength, endCh: make(chan struct{}, 1)}
}

// load starts subtune of f from the beginning.
func (e *engine) load(f *sndh.File, subtune int) error {
	p, err := newTunePlayer(f, subtune, e.rate)
	if err != nil {
		return err
	}
	t := &tap{src: p}
	split, out, err := buildChain(t, e.pan.panning())
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.player, e.tap, e.split = p, t, split
	e.reader = audiostream.NewStereoReader(out, 512)
	e.paused, e.ended = false, false
	return nil
}

// length is when the current tune counts as finished, 0 for never.
func (e *engine) lengthLocked() time.Duration {
	if e.player == nil || e.mode == modeLoop {
		return 0
	}
	if d := e.player.Duration(); d > 0 {
		return d
	}
	return e.defaultLength
}

// Read implements io.Reader for the audio device.
func (e *engine) Read(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.player == nil || e.paused || e.ended {
		clear(p)
		return len(p), nil
	}
	n, err := e.reader.Read(p)
	if l := e.lengthLocked(); (l > 0 && e.player.Position() >= l) || e.player.Finished() {
		e.ended = true
		select {
		case e.endCh <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (e *engine) togglePause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.player == nil {
		return
	}
	if e.ended { // replay a finished tune from the start
		e.player.Seek(0)
		e.ended, e.paused = false, false
		return
	}
	e.paused = !e.paused
}

func (e *engine) seek(d time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.player == nil {
		return nil
	}
	d = max(d, 0)
	if l := e.lengthLocked(); l > 0 {
		d = min(d, l)
	}
	e.ended = false
	return e.player.Seek(d)
}

func (e *engine) setMode(m playMode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = m
}

func (e *engine) setPan(p panMode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pan = p
	if e.split != nil {
		e.split.SetPanning(p.panning())
	}
}

// status is a consistent snapshot for one UI frame.
type status struct {
	loaded   bool
	paused   bool
	ended    bool
	position time.Duration
	length   time.Duration // effective end, 0 = endless
	duration time.Duration // from the header, 0 = unknown
	mode     playMode
	pan      panMode
	err      error
	underrun uint64
	scope    [3][]float32
}

func (e *engine) snapshot(n int) status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := status{mode: e.mode, pan: e.pan, paused: e.paused, ended: e.ended}
	if e.player == nil {
		return s
	}
	s.loaded = true
	s.position = e.player.Position()
	s.length = e.lengthLocked()
	s.duration = e.player.Duration()
	s.err = e.player.Err()
	s.underrun = e.reader.Underruns()
	n = min(n, scopeLen)
	for ch := range 3 {
		buf := make([]float32, n)
		start := (e.tap.pos[ch] - n + scopeLen) % scopeLen
		for i := range buf {
			buf[i] = e.tap.ring[ch][(start+i)%scopeLen]
		}
		s.scope[ch] = buf
	}
	return s
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i]|0x20, b[i]|0x20
		if x != y {
			return false
		}
	}
	return true
}
