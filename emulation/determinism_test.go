package ym2149

import (
	"math"
	"slices"
	"testing"
)

type timelineEvent struct {
	cycle uint64
	reg   byte
	val   byte
}

func TestPCMDeterminismAcrossStepChunking(t *testing.T) {
	events := []timelineEvent{
		{cycle: 0, reg: 0, val: 0x20},
		{cycle: 0, reg: 1, val: 0x01},
		{cycle: 0, reg: 7, val: 0x3e},
		{cycle: 0, reg: 8, val: 0x0f},
		{cycle: 2048, reg: 8, val: 0x10},
		{cycle: 2048, reg: 11, val: 0x04},
		{cycle: 2048, reg: 12, val: 0x00},
		{cycle: 2048, reg: 13, val: 0x0d},
		{cycle: 8192, reg: 6, val: 0x03},
		{cycle: 8192, reg: 7, val: 0x36},
		{cycle: 12000, reg: 8, val: 0x0a},
	}

	oneCycle := func(remaining uint64) uint32 { return 1 }
	bursty := func(remaining uint64) uint32 {
		chunk := []uint32{1, 13, 64, 257, 7}[remaining%5]
		if uint64(chunk) > remaining {
			return uint32(remaining)
		}
		return chunk
	}

	a := runTimeline(events, 20_000, oneCycle)
	b := runTimeline(events, 20_000, bursty)

	if len(a) != len(b) {
		t.Fatalf("sample count mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-7 {
			t.Fatalf("sample[%d] mismatch: %.8f vs %.8f", i, a[i], b[i])
		}
	}
}

func TestDrainMonoF32ReturnsQueuedSamples(t *testing.T) {
	chip := New(Config{})
	chip.SelectRegister(0)
	chip.WriteData(0x10)
	chip.SelectRegister(1)
	chip.WriteData(0x00)
	chip.SelectRegister(7)
	chip.WriteData(0x3e)
	chip.SelectRegister(8)
	chip.WriteData(0x0f)

	chip.Step(12_000)
	before := chip.BufferedSamples()
	if before == 0 {
		t.Fatal("expected buffered samples after stepping")
	}

	dst := make([]float32, before+8)
	n := chip.DrainMonoF32(dst)
	if n != before {
		t.Fatalf("DrainMonoF32 = %d, want %d", n, before)
	}
	if chip.BufferedSamples() != 0 {
		t.Fatal("expected queue to be empty after drain")
	}
	if slices.Equal(dst[:n], make([]float32, n)) {
		t.Fatal("expected non-zero PCM data")
	}
}

func runTimeline(events []timelineEvent, endCycle uint64, nextChunk func(remaining uint64) uint32) []float32 {
	chip := New(Config{})
	current := uint64(0)
	index := 0

	for current < endCycle {
		for index < len(events) && events[index].cycle == current {
			chip.SelectRegister(events[index].reg)
			chip.WriteData(events[index].val)
			index++
		}

		nextEvent := endCycle
		if index < len(events) {
			nextEvent = events[index].cycle
		}
		remaining := nextEvent - current
		if remaining == 0 {
			continue
		}
		chunk := nextChunk(remaining)
		chip.Step(chunk)
		current += uint64(chunk)
	}

	out := make([]float32, chip.BufferedSamples())
	n := chip.DrainMonoF32(out)
	return out[:n]
}
