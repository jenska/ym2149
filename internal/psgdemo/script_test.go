package psgdemo

import (
	"testing"

	ym2149 "ym2149/emulation"
)

func TestDefaultSequenceAdvancesAndWritesRegisters(t *testing.T) {
	chip := ym2149.New(ym2149.Config{})
	seq := NewSequencer(DefaultSequence())
	seq.Reset(chip)

	if seq.CurrentName() == "none" {
		t.Fatal("expected a scripted step")
	}

	initialReg := readReg(chip, 0)
	for i := 0; i < 200; i++ {
		seq.Tick(chip)
	}
	if got := readReg(chip, 0); got == initialReg {
		t.Fatalf("expected scripted sequence to change tone register, still 0x%02x", got)
	}
}

func TestTonePeriodForFrequencyBounds(t *testing.T) {
	if got := TonePeriodForFrequency(2_000_000, 0); got != 1 {
		t.Fatalf("TonePeriodForFrequency(0) = %d, want 1", got)
	}
	if got := TonePeriodForFrequency(2_000_000, 0.01); got != 0x0fff {
		t.Fatalf("TonePeriodForFrequency(low) = 0x%03x, want 0x0fff", got)
	}
}

func readReg(chip *ym2149.Chip, reg byte) byte {
	chip.SelectRegister(reg)
	return chip.ReadData()
}
