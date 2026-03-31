package ym2149

import "testing"

type busWrite struct {
	cycle uint64
	reg   byte
	val   byte
}

type chipSnapshot struct {
	cycles       uint64
	envVolume    int
	envStep      int
	envCounter   uint16
	noiseLFSR    uint32
	noiseOutput  bool
	noiseCounter uint8
}

func TestBusTimestampedEnvelopeRestartTiming(t *testing.T) {
	beforeTick := runBusSnapshots(
		[]busWrite{
			{cycle: 0, reg: 11, val: 1},
			{cycle: 0, reg: 12, val: 0},
			{cycle: 0, reg: 13, val: 0x08},
			{cycle: 31, reg: 13, val: 0x08},
		},
		[]uint64{32, 40},
	)
	afterTick := runBusSnapshots(
		[]busWrite{
			{cycle: 0, reg: 11, val: 1},
			{cycle: 0, reg: 12, val: 0},
			{cycle: 0, reg: 13, val: 0x08},
			{cycle: 32, reg: 13, val: 0x08},
		},
		[]uint64{32, 40},
	)

	if got := beforeTick[32].envVolume; got != 30 {
		t.Fatalf("restart before tick envVolume@32 = %d, want 30", got)
	}
	if got := beforeTick[40].envVolume; got != 29 {
		t.Fatalf("restart before tick envVolume@40 = %d, want 29", got)
	}

	if got := afterTick[32].envVolume; got != 31 {
		t.Fatalf("restart after tick envVolume@32 = %d, want 31", got)
	}
	if got := afterTick[40].envVolume; got != 30 {
		t.Fatalf("restart after tick envVolume@40 = %d, want 30", got)
	}
}

func TestBusTimestampedNoisePeriodWriteTiming(t *testing.T) {
	beforeTick := runBusSnapshots(
		[]busWrite{
			{cycle: 0, reg: 6, val: 5},
			{cycle: 31, reg: 6, val: 4},
		},
		[]uint64{32, 40},
	)
	afterTick := runBusSnapshots(
		[]busWrite{
			{cycle: 0, reg: 6, val: 5},
			{cycle: 32, reg: 6, val: 4},
		},
		[]uint64{32, 40},
	)

	if got := beforeTick[32].noiseLFSR; got != 0x0ffff {
		t.Fatalf("write before tick noiseLFSR@32 = 0x%x, want 0x0ffff", got)
	}
	if got := beforeTick[32].noiseCounter; got != 0 {
		t.Fatalf("write before tick noiseCounter@32 = %d, want 0", got)
	}
	if got := beforeTick[40].noiseLFSR; got != 0x0ffff {
		t.Fatalf("write before tick noiseLFSR@40 = 0x%x, want 0x0ffff", got)
	}

	if got := afterTick[32].noiseLFSR; got != 0x1ffff {
		t.Fatalf("write after tick noiseLFSR@32 = 0x%x, want 0x1ffff", got)
	}
	if got := afterTick[32].noiseCounter; got != 4 {
		t.Fatalf("write after tick noiseCounter@32 = %d, want 4", got)
	}
	if got := afterTick[40].noiseLFSR; got != 0x0ffff {
		t.Fatalf("write after tick noiseLFSR@40 = 0x%x, want 0x0ffff", got)
	}
}

func TestBusTimestampedEnvelopeZeroPeriodCornerCase(t *testing.T) {
	snapshots := runBusSnapshots(
		[]busWrite{
			{cycle: 0, reg: 11, val: 0},
			{cycle: 0, reg: 12, val: 0},
			{cycle: 0, reg: 13, val: 0x08},
		},
		[]uint64{8, 16},
	)

	if got := snapshots[8].envVolume; got != 30 {
		t.Fatalf("zero envelope period envVolume@8 = %d, want 30", got)
	}
	if got := snapshots[16].envVolume; got != 29 {
		t.Fatalf("zero envelope period envVolume@16 = %d, want 29", got)
	}
}

func TestBusTimestampedNoiseZeroPeriodCornerCase(t *testing.T) {
	snapshots := runBusSnapshots(
		[]busWrite{
			{cycle: 0, reg: 6, val: 0},
		},
		[]uint64{8, 16},
	)

	if got := snapshots[8].noiseLFSR; got != 0x0ffff {
		t.Fatalf("zero noise period noiseLFSR@8 = 0x%x, want 0x0ffff", got)
	}
	if got := snapshots[16].noiseLFSR; got != 0x07fff {
		t.Fatalf("zero noise period noiseLFSR@16 = 0x%x, want 0x07fff", got)
	}
}

func runBusSnapshots(writes []busWrite, checkpoints []uint64) map[uint64]chipSnapshot {
	chip := New(Config{})
	snapshots := make(map[uint64]chipSnapshot, len(checkpoints))

	current := uint64(0)
	writeIndex := 0
	checkIndex := 0

	for writeIndex < len(writes) || checkIndex < len(checkpoints) {
		nextCycle := uint64(^uint64(0))
		if writeIndex < len(writes) && writes[writeIndex].cycle < nextCycle {
			nextCycle = writes[writeIndex].cycle
		}
		if checkIndex < len(checkpoints) && checkpoints[checkIndex] < nextCycle {
			nextCycle = checkpoints[checkIndex]
		}

		if nextCycle > current {
			chip.Step(uint32(nextCycle - current))
			current = nextCycle
		}

		for writeIndex < len(writes) && writes[writeIndex].cycle == current {
			chip.SelectRegister(writes[writeIndex].reg)
			chip.WriteData(writes[writeIndex].val)
			writeIndex++
		}

		for checkIndex < len(checkpoints) && checkpoints[checkIndex] == current {
			snapshots[current] = chipSnapshot{
				cycles:       chip.cycles,
				envVolume:    chip.envVolume,
				envStep:      chip.envStep,
				envCounter:   chip.envCounter,
				noiseLFSR:    chip.noiseLFSR,
				noiseOutput:  chip.noiseOutput,
				noiseCounter: chip.noiseCounter,
			}
			checkIndex++
		}
	}

	return snapshots
}
