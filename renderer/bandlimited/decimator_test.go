package bandlimited

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

func TestOutputSampleRateIsDecimated(t *testing.T) {
	src := &fakeSource{sampleRate: 192_000}
	decimator, err := New(src, Config{OversampleFactor: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := decimator.OutputSampleRate(); got != 48_000 {
		t.Fatalf("OutputSampleRate = %d, want 48000", got)
	}
}

func TestDCGainStaysNearUnity(t *testing.T) {
	src := &fakeSource{
		samples:    repeat(1, 4096),
		sampleRate: 192_000,
	}
	decimator, err := New(src, Config{OversampleFactor: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out := make([]float32, 1024)
	n := decimator.DrainMonoF32(out)
	if n != len(out) {
		t.Fatalf("DrainMonoF32 = %d, want %d", n, len(out))
	}

	mean := average(out[128:])
	if math.Abs(mean-1.0) > 0.03 {
		t.Fatalf("DC gain mean = %f, want near 1.0", mean)
	}
}

func TestHighFrequencyIsAttenuated(t *testing.T) {
	src := &fakeSource{
		samples:    sineWave(192_000, 60_000, 8192),
		sampleRate: 192_000,
	}
	decimator, err := New(src, Config{OversampleFactor: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out := make([]float32, 2048)
	n := decimator.DrainMonoF32(out)
	if n != len(out) {
		t.Fatalf("DrainMonoF32 = %d, want %d", n, len(out))
	}

	if got := rms(out[128:]); got > 0.15 {
		t.Fatalf("expected strong attenuation above output Nyquist, rms=%f", got)
	}
}

func repeat(v float32, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func sineWave(sampleRate int, freq float64, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		phase := 2 * math.Pi * freq * float64(i) / float64(sampleRate)
		out[i] = float32(math.Sin(phase))
	}
	return out
}

func average(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, sample := range samples {
		sum += float64(sample)
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
