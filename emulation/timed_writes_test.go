package ym2149

import "testing"

func TestWriteAtAppliesAtScheduledCycle(t *testing.T) {
	chip := New(Config{ClockHz: 2_000_000, OutputSampleRate: 50_000})

	chip.WriteAt(1000, 8, 0x0f)
	chip.WriteAt(500, 0, 0x40)
	chip.WriteAt(1500, 7, 0x3e)

	if got := chip.PendingWrites(); got != 3 {
		t.Fatalf("PendingWrites = %d, want 3", got)
	}

	chip.Step(400)
	if reg := readReg(chip, 0); reg != 0 {
		t.Fatalf("after 400 cycles reg0 = %#x, want 0", reg)
	}

	chip.Step(200) // now at cycle 600: the cycle-500 write has fired
	if reg := readReg(chip, 0); reg != 0x40 {
		t.Fatalf("after 600 cycles reg0 = %#x, want 0x40", reg)
	}
	if reg := readReg(chip, 8); reg != 0 {
		t.Fatalf("after 600 cycles reg8 = %#x, want 0", reg)
	}

	chip.Step(1000) // now at cycle 1600: all three have fired
	if reg := readReg(chip, 8); reg != 0x0f {
		t.Fatalf("reg8 = %#x, want 0x0f", reg)
	}
	if reg := readReg(chip, 7); reg != 0x3e {
		t.Fatalf("reg7 = %#x, want 0x3e", reg)
	}
	if got := chip.PendingWrites(); got != 0 {
		t.Fatalf("PendingWrites = %d, want 0", got)
	}
}

func TestWriteAtPastDueAppliesOnNextStep(t *testing.T) {
	chip := New(Config{})
	chip.Step(5000)

	chip.WriteAt(10, 8, 0x0c) // already in the past
	if reg := readReg(chip, 8); reg != 0 {
		t.Fatalf("reg8 applied too early: %#x", reg)
	}
	chip.Step(1)
	if reg := readReg(chip, 8); reg != 0x0c {
		t.Fatalf("past-due write not applied: reg8 = %#x", reg)
	}
}

func TestWriteAtOrderingAndClear(t *testing.T) {
	chip := New(Config{})

	// Same target cycle: writes apply in scheduling order (last wins on reg 8).
	chip.WriteAt(100, 8, 0x01)
	chip.WriteAt(100, 8, 0x02)
	chip.WriteAt(100, 8, 0x03)
	chip.WriteAt(200, 8, 0x09)

	chip.ClearPendingWrites()
	if got := chip.PendingWrites(); got != 0 {
		t.Fatalf("ClearPendingWrites left %d", got)
	}

	chip.WriteAt(100, 8, 0x01)
	chip.WriteAt(100, 8, 0x02)
	chip.WriteAt(100, 8, 0x03)
	chip.Step(150)
	if reg := readReg(chip, 8); reg != 0x03 {
		t.Fatalf("reg8 = %#x, want 0x03 (last scheduled wins)", reg)
	}
}

func TestWriteImmediateMatchesLatchWrite(t *testing.T) {
	a := New(Config{})
	b := New(Config{})

	a.SelectRegister(13)
	a.WriteData(0x0c)
	b.Write(13, 0x0c)

	a.Step(4000)
	b.Step(4000)

	sa := make([]float32, a.BufferedSamples())
	sb := make([]float32, b.BufferedSamples())
	a.DrainMonoF32(sa)
	b.DrainMonoF32(sb)
	if len(sa) != len(sb) {
		t.Fatalf("sample counts differ: %d vs %d", len(sa), len(sb))
	}
	for i := range sa {
		if sa[i] != sb[i] {
			t.Fatalf("sample %d: %v vs %v", i, sa[i], sb[i])
		}
	}
}

func TestResetClearsPendingWrites(t *testing.T) {
	chip := New(Config{})
	chip.WriteAt(1_000_000, 8, 0x0f)
	chip.Reset()
	if got := chip.PendingWrites(); got != 0 {
		t.Fatalf("Reset left %d pending writes", got)
	}
}

// TestConcurrentWriteAtAndStep is a race-detector target: a producer goroutine
// queues timestamped writes while a consumer advances the chip and drains PCM.
func TestConcurrentWriteAtAndStep(t *testing.T) {
	chip := New(Config{ClockHz: 2_000_000, OutputSampleRate: 48_000, ChannelTaps: true})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 2000 {
			chip.WriteAt(chip.Cycles()+uint64(i%64), byte(i%14), byte(i))
		}
	}()

	buf := make([]float32, 1024)
	for range 200 {
		chip.Step(2000)
		chip.DrainMonoF32(buf)
		chip.DrainChannelF32(1, buf)
	}
	<-done
}

func readReg(c *Chip, reg byte) byte {
	c.SelectRegister(reg)
	return c.ReadData()
}
