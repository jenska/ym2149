package ym2149

import (
	"math"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	chip := New(Config{})

	if got := chip.ClockHz(); got != defaultClockHz {
		t.Fatalf("ClockHz = %d, want %d", got, defaultClockHz)
	}
	if got := chip.OutputSampleRate(); got != defaultOutputSampleRate {
		t.Fatalf("OutputSampleRate = %d, want %d", got, defaultOutputSampleRate)
	}
	if got := chip.BufferedSamples(); got != 0 {
		t.Fatalf("BufferedSamples = %d, want 0", got)
	}
}

func TestNewWithDefaultsUsesProvidedRatesAndDefaultBuffer(t *testing.T) {
	chip := NewWithDefaults(1_789_772, 44_100)

	if got := chip.ClockHz(); got != 1_789_772 {
		t.Fatalf("ClockHz = %d, want %d", got, 1_789_772)
	}
	if got := chip.OutputSampleRate(); got != 44_100 {
		t.Fatalf("OutputSampleRate = %d, want %d", got, 44_100)
	}
	if got := len(chip.samples.data); got != defaultBufferSamples {
		t.Fatalf("buffer length = %d, want %d", got, defaultBufferSamples)
	}
}

func TestRegisterReadbackAndMasking(t *testing.T) {
	chip := New(Config{})

	chip.SelectRegister(1)
	chip.WriteData(0xff)
	if got := chip.ReadData(); got != 0xff {
		t.Fatalf("ReadData coarse tone = 0x%02x, want 0xff", got)
	}

	chip.mu.Lock()
	period := chip.tonePeriodLocked(0)
	chip.mu.Unlock()
	if period != 0x0f00 {
		t.Fatalf("tonePeriodLocked = 0x%03x, want 0x0f00", period)
	}

	chip.SelectRegister(6)
	chip.WriteData(0xe0)
	chip.mu.Lock()
	noisePeriod := chip.noisePeriodLocked()
	chip.mu.Unlock()
	if noisePeriod != 1 {
		t.Fatalf("noisePeriodLocked = %d, want 1", noisePeriod)
	}
}

func TestPortDirectionAndReadback(t *testing.T) {
	chip := New(Config{})

	chip.SelectRegister(14)
	chip.WriteData(0xa5)
	chip.SelectRegister(15)
	chip.WriteData(0x5a)

	chip.SelectRegister(7)
	chip.WriteData(0x00)

	ports := chip.Ports()
	if ports.AOutput != 0xa5 || ports.BOutput != 0x5a {
		t.Fatalf("unexpected output ports: %+v", ports)
	}

	chip.SetPortAInput(0x3c)
	chip.SetPortBInput(0xc3)
	chip.SelectRegister(7)
	chip.WriteData(0xc0)

	ports = chip.Ports()
	if ports.AOutput != 0 || ports.BOutput != 0 {
		t.Fatalf("expected output drive to be disabled, got %+v", ports)
	}

	chip.SelectRegister(14)
	if got := chip.ReadData(); got != 0x3c {
		t.Fatalf("port A input readback = 0x%02x, want 0x3c", got)
	}
	chip.SelectRegister(15)
	if got := chip.ReadData(); got != 0xc3 {
		t.Fatalf("port B input readback = 0x%02x, want 0xc3", got)
	}
}

func TestToneAndNoiseAdvanceAtExpectedCadence(t *testing.T) {
	chip := New(Config{})

	chip.SelectRegister(0)
	chip.WriteData(1)
	chip.SelectRegister(1)
	chip.WriteData(0)
	chip.SelectRegister(6)
	chip.WriteData(1)

	if !chip.toneOutputs[0] {
		t.Fatal("expected tone output to start high")
	}
	initialLFSR := chip.noiseLFSR

	chip.Step(8)
	if chip.toneOutputs[0] {
		t.Fatal("expected tone output to toggle after 8 clocks")
	}
	if chip.noiseLFSR == initialLFSR {
		t.Fatal("expected noise LFSR to advance after 8 clocks")
	}

	chip.Step(8)
	if !chip.toneOutputs[0] {
		t.Fatal("expected tone output to toggle back after 16 clocks")
	}
}

func TestAnalogMixerSilenceIsZeroReferenced(t *testing.T) {
	if got := ym2149AnalogMixLevels[analogMixIndex(0, [3]int{})]; got != 0 {
		t.Fatalf("silence level = %f, want 0", got)
	}
}

func TestAnalogMixerIsNotLinearAverage(t *testing.T) {
	single := float64(ym2149AnalogMixLevels[analogMixIndex(0, [3]int{15, 0, 0})])
	double := float64(ym2149AnalogMixLevels[analogMixIndex(0, [3]int{15, 15, 0})])

	if !(double > single) {
		t.Fatalf("expected two active channels to be louder than one: single=%f double=%f", single, double)
	}
	if almostEqual(double, single*2) || math.Abs(double-single*2) < 1e-3 {
		t.Fatalf("expected nonlinear analog mix, got single=%f double=%f", single, double)
	}
}
