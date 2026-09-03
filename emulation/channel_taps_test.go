package ym2149

import (
	"math"
	"testing"
)

func rmsOf(s []float32) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

func TestChannelTapsDisabledByDefault(t *testing.T) {
	chip := New(Config{})
	if chip.ChannelTapsEnabled() {
		t.Fatal("channel taps should be off by default")
	}
	chip.Write(7, 0x3e)
	chip.Write(8, 0x0f)
	chip.Step(4000)

	buf := make([]float32, 256)
	if n := chip.DrainChannelF32(0, buf); n != 0 {
		t.Fatalf("DrainChannelF32 returned %d with taps disabled", n)
	}
}

func TestChannelTapsIsolateVoices(t *testing.T) {
	chip := New(Config{ClockHz: 2_000_000, OutputSampleRate: 96_000, ChannelTaps: true})

	// Square wave on channel B only.
	chip.Write(2, 0x40) // B tone period low
	chip.Write(3, 0x00)
	chip.Write(7, 0x3d) // mixer: enable tone B (bit 1 clear), everything else off
	chip.Write(9, 0x0f) // B volume max

	chip.Step(2_000_000) // one second

	buffered := chip.BufferedChannelSamples()
	var got [3][]float32
	for ch := range 3 {
		got[ch] = make([]float32, buffered)
		n := chip.DrainChannelF32(ch, got[ch])
		got[ch] = got[ch][:n]
	}

	if rmsOf(got[1]) < 0.05 {
		t.Fatalf("channel B tap is silent: rms=%.4f", rmsOf(got[1]))
	}
	if rmsOf(got[0]) > 1e-4 || rmsOf(got[2]) > 1e-4 {
		t.Fatalf("channels A/C should be silent: A=%.6f C=%.6f", rmsOf(got[0]), rmsOf(got[2]))
	}
}

func TestChannelTapsTrackMonoBuffer(t *testing.T) {
	chip := New(Config{OutputSampleRate: 48_000, ChannelTaps: true})
	chip.Write(7, 0x38)
	chip.Write(8, 0x0a)
	chip.Write(0, 0x80)
	chip.Step(200_000)

	if mono, ch := chip.BufferedSamples(), chip.BufferedChannelSamples(); mono != ch {
		t.Fatalf("mono buffer %d != channel buffer %d", mono, ch)
	}
}

func TestChannelTapsResetClears(t *testing.T) {
	chip := New(Config{ChannelTaps: true})
	chip.Write(7, 0x38)
	chip.Write(8, 0x0f)
	chip.Step(50_000)
	if chip.BufferedChannelSamples() == 0 {
		t.Fatal("expected buffered channel samples before reset")
	}
	chip.Reset()
	if got := chip.BufferedChannelSamples(); got != 0 {
		t.Fatalf("reset left %d channel samples", got)
	}
}
