package stereo_test

import (
	"math"
	"testing"

	ym2149 "github.com/jenska/ym2149/emulation"
	"github.com/jenska/ym2149/renderer/stereo"
)

func rms(s []float32) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

// A tone on channel A, panned hard left, should be loud on the left output and
// near-silent on the right.
func TestChipToStereoHardPan(t *testing.T) {
	chip := ym2149.New(ym2149.Config{
		ClockHz:          2_000_000,
		OutputSampleRate: 96_000,
		ChannelTaps:      true,
	})
	chip.Write(0, 0x80) // channel A tone period
	chip.Write(7, 0x3e) // mixer: tone A only
	chip.Write(8, 0x0f) // A volume max
	chip.Step(2_000_000)

	sp := stereo.NewSplitter(chip, stereo.ABC())
	left := make([]float32, 20_000)
	right := make([]float32, 20_000)
	nl := sp.Left().DrainMonoF32(left)
	nr := sp.Right().DrainMonoF32(right)
	if nl == 0 || nr == 0 {
		t.Fatalf("drained %d / %d", nl, nr)
	}

	lr, rr := rms(left[:nl]), rms(right[:nr])
	if lr < 0.05 {
		t.Fatalf("left output too quiet: %.4f", lr)
	}
	if rr > lr/10 {
		t.Fatalf("right output should be near-silent: left=%.4f right=%.4f", lr, rr)
	}
}

func TestChipToStereoInterleavedRunsClean(t *testing.T) {
	chip := ym2149.New(ym2149.Config{OutputSampleRate: 48_000, ChannelTaps: true})
	chip.Write(0, 0x40)
	chip.Write(2, 0x60)
	chip.Write(7, 0x3c) // tone A + B
	chip.Write(8, 0x0c)
	chip.Write(9, 0x0c)
	chip.Step(1_000_000)

	sp := stereo.NewSplitter(chip, stereo.ACBWidth(0.6))
	out := stereo.Interleave(sp.Left(), sp.Right())

	buf := make([]float32, 4096)
	total := 0
	for range 20 {
		total += out.DrainStereoF32(buf)
	}
	if total == 0 {
		t.Fatal("interleaved stereo produced nothing")
	}
	if total%2 != 0 {
		t.Fatalf("odd interleaved count %d", total)
	}
}
