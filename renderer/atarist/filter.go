package atarist

import "math"

const (
	defaultHighPassHz = 30.0
	defaultLowPassHz  = 7_500.0
	defaultGain       = 1.0
)

// MonoSource is the minimum interface required for ST board-level filtering.
type MonoSource interface {
	DrainMonoF32([]float32) int
	OutputSampleRate() int
}

// Config controls the approximate Atari ST board output stage.
//
// The defaults model a simple AC-coupled output with gentle treble roll-off.
// This is an ST-style board stage approximation rather than a claim of exact
// per-revision analog reproduction.
type Config struct {
	HighPassHz float64
	LowPassHz  float64
	Gain       float64
}

// Output wraps a mono source with a lightweight ST-style board-output stage.
type Output struct {
	source MonoSource
	cfg    Config

	hp highPass
	lp lowPass
}

// New wraps a chip-level mono source with Atari ST style output shaping.
func New(source MonoSource, cfg Config) *Output {
	cfg = cfg.withDefaults()
	sampleRate := float64(source.OutputSampleRate())
	return &Output{
		source: source,
		cfg:    cfg,
		hp:     newHighPass(sampleRate, cfg.HighPassHz),
		lp:     newLowPass(sampleRate, cfg.LowPassHz),
	}
}

// OutputSampleRate returns the wrapped source sample rate.
func (o *Output) OutputSampleRate() int {
	return o.source.OutputSampleRate()
}

// DrainMonoF32 copies filtered mono samples into dst.
func (o *Output) DrainMonoF32(dst []float32) int {
	n := o.source.DrainMonoF32(dst)
	for i := 0; i < n; i++ {
		sample := float64(dst[i]) * o.cfg.Gain
		sample = o.hp.step(sample)
		sample = o.lp.step(sample)
		dst[i] = float32(sample)
	}
	return n
}

func (cfg Config) withDefaults() Config {
	if cfg.HighPassHz <= 0 {
		cfg.HighPassHz = defaultHighPassHz
	}
	if cfg.LowPassHz <= 0 {
		cfg.LowPassHz = defaultLowPassHz
	}
	if cfg.Gain == 0 {
		cfg.Gain = defaultGain
	}
	return cfg
}

type highPass struct {
	alpha float64
	prevX float64
	prevY float64
}

func newHighPass(sampleRate, cutoff float64) highPass {
	if cutoff <= 0 || sampleRate <= 0 {
		return highPass{alpha: 1}
	}
	rc := 1.0 / (2.0 * math.Pi * cutoff)
	dt := 1.0 / sampleRate
	return highPass{alpha: rc / (rc + dt)}
}

func (f *highPass) step(x float64) float64 {
	y := f.alpha * (f.prevY + x - f.prevX)
	f.prevX = x
	f.prevY = y
	return y
}

type lowPass struct {
	alpha float64
	y     float64
}

func newLowPass(sampleRate, cutoff float64) lowPass {
	if cutoff <= 0 || sampleRate <= 0 {
		return lowPass{alpha: 1}
	}
	rc := 1.0 / (2.0 * math.Pi * cutoff)
	dt := 1.0 / sampleRate
	return lowPass{alpha: dt / (rc + dt)}
}

func (f *lowPass) step(x float64) float64 {
	f.y += f.alpha * (x - f.y)
	return f.y
}
