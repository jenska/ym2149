package atarist

import (
	"math"
	"testing"
)

type fakeSource struct {
	samples    []float32
	sampleRate int
}

func (f *fakeSource) DrainMonoF32(dst []float32) int {
	n := len(dst)
	if n > len(f.samples) {
		n = len(f.samples)
	}
	copy(dst, f.samples[:n])
	f.samples = f.samples[n:]
	return n
}

func (f *fakeSource) OutputSampleRate() int {
	return f.sampleRate
}

func TestHighPassRemovesDC(t *testing.T) {
	src := &fakeSource{
		samples:    repeat(1.0, 2048),
		sampleRate: 48_000,
	}
	filtered := New(src, Config{})
	buf := make([]float32, 2048)
	n := filtered.DrainMonoF32(buf)
	if n != len(buf) {
		t.Fatalf("DrainMonoF32 = %d, want %d", n, len(buf))
	}

	tailMean := meanAbs(buf[len(buf)-256:])
	if tailMean > 0.05 {
		t.Fatalf("expected DC to decay toward zero, tail mean=%f", tailMean)
	}
}

func TestLowPassAttenuatesHighFrequency(t *testing.T) {
	raw := squareWave(48_000, 10_000, 2048)
	src := &fakeSource{
		samples:    append([]float32(nil), raw...),
		sampleRate: 48_000,
	}
	filtered := New(src, Config{})
	buf := make([]float32, len(raw))
	filtered.DrainMonoF32(buf)

	rawRMS := rms(raw[256:])
	filteredRMS := rms(buf[256:])
	if !(filteredRMS < rawRMS) {
		t.Fatalf("expected low-pass attenuation, raw=%f filtered=%f", rawRMS, filteredRMS)
	}
}

func repeat(v float32, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func squareWave(sampleRate int, freq float64, n int) []float32 {
	out := make([]float32, n)
	phase := 0.0
	step := freq / float64(sampleRate)
	for i := range out {
		if phase < 0.5 {
			out[i] = 1
		} else {
			out[i] = -1
		}
		phase += step
		if phase >= 1 {
			phase -= 1
		}
	}
	return out
}

func meanAbs(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, sample := range samples {
		sum += math.Abs(float64(sample))
	}
	return sum / float64(len(samples))
}

func rms(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, sample := range samples {
		sum += float64(sample * sample)
	}
	return math.Sqrt(sum / float64(len(samples)))
}
