package bandlimited

import (
	"fmt"
	"math"
)

const (
	defaultOversampleFactor = 4
	defaultFIRLength        = 63
	defaultCutoffRatio      = 0.90
)

// MonoSource is the minimum interface required for band-limited decimation.
type MonoSource interface {
	DrainMonoF32([]float32) int
	OutputSampleRate() int
}

// Config controls FIR-based oversampling decimation.
type Config struct {
	OversampleFactor int
	FIRLength        int
	CutoffRatio      float64
}

// Decimator downsamples an oversampled mono source through a low-pass FIR.
type Decimator struct {
	source MonoSource
	cfg    Config

	taps       []float64
	delayLine  []float64
	delayIndex int
	phase      int
	inputBuf   []float32
}

// New wraps an oversampled mono source and exposes a lower output sample rate.
//
// The wrapped source must already be configured to render at
// targetRate * OversampleFactor.
func New(source MonoSource, cfg Config) (*Decimator, error) {
	cfg = cfg.withDefaults()
	if source.OutputSampleRate()%cfg.OversampleFactor != 0 {
		return nil, fmt.Errorf("source sample rate %d is not divisible by oversample factor %d", source.OutputSampleRate(), cfg.OversampleFactor)
	}

	return &Decimator{
		source:    source,
		cfg:       cfg,
		taps:      buildFIR(cfg.OversampleFactor, cfg.FIRLength, cfg.CutoffRatio),
		delayLine: make([]float64, cfg.FIRLength),
		inputBuf:  make([]float32, cfg.OversampleFactor*256),
	}, nil
}

// OutputSampleRate returns the decimated output sample rate.
func (d *Decimator) OutputSampleRate() int {
	return d.source.OutputSampleRate() / d.cfg.OversampleFactor
}

// DrainMonoF32 copies decimated mono samples into dst.
func (d *Decimator) DrainMonoF32(dst []float32) int {
	produced := 0

	for produced < len(dst) {
		read := d.source.DrainMonoF32(d.inputBuf)
		if read == 0 {
			break
		}

		for i := 0; i < read && produced < len(dst); i++ {
			d.push(float64(d.inputBuf[i]))
			d.phase++
			if d.phase == d.cfg.OversampleFactor {
				d.phase = 0
				dst[produced] = float32(d.convolve())
				produced++
			}
		}
	}

	return produced
}

func (cfg Config) withDefaults() Config {
	if cfg.OversampleFactor <= 0 {
		cfg.OversampleFactor = defaultOversampleFactor
	}
	if cfg.FIRLength <= 0 {
		cfg.FIRLength = defaultFIRLength
	}
	if cfg.FIRLength%2 == 0 {
		cfg.FIRLength++
	}
	if cfg.CutoffRatio <= 0 || cfg.CutoffRatio >= 1 {
		cfg.CutoffRatio = defaultCutoffRatio
	}
	return cfg
}

func (d *Decimator) push(sample float64) {
	d.delayLine[d.delayIndex] = sample
	d.delayIndex++
	if d.delayIndex == len(d.delayLine) {
		d.delayIndex = 0
	}
}

func (d *Decimator) convolve() float64 {
	sum := 0.0
	idx := d.delayIndex
	for i, tap := range d.taps {
		idx--
		if idx < 0 {
			idx = len(d.delayLine) - 1
		}
		sum += d.delayLine[idx] * tap
		if i == len(d.taps)-1 {
			break
		}
	}
	return sum
}

func buildFIR(factor, length int, cutoffRatio float64) []float64 {
	taps := make([]float64, length)
	center := float64(length-1) / 2.0
	cutoff := 0.5 * cutoffRatio / float64(factor)
	sum := 0.0

	for i := range taps {
		x := float64(i) - center
		window := 0.42 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(length-1)) + 0.08*math.Cos(4*math.Pi*float64(i)/float64(length-1))

		var sinc float64
		if x == 0 {
			sinc = 2 * cutoff
		} else {
			sinc = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}

		taps[i] = sinc * window
		sum += taps[i]
	}

	if sum == 0 {
		return taps
	}
	for i := range taps {
		taps[i] /= sum
	}
	return taps
}
