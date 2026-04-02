package ym2149

// ClockDomain converts cycles from one clock domain into another using exact
// integer accumulation. This is useful for hosts that need to translate CPU or
// bus cycles into PSG master clock cycles without losing fractional progress.
type ClockDomain struct {
	sourceHz  uint64
	targetHz  uint64
	remainder uint64
}

// NewClockDomain constructs a cycle converter from sourceHz to targetHz.
func NewClockDomain(sourceHz, targetHz int) *ClockDomain {
	return &ClockDomain{
		sourceHz: uint64(clampPositive(sourceHz)),
		targetHz: uint64(clampPositive(targetHz)),
	}
}

// NewPSGClockDomain is a convenience wrapper for converting host cycles into
// YM2149 master clock cycles.
func NewPSGClockDomain(hostHz, psgHz int) *ClockDomain {
	return NewClockDomain(hostHz, psgHz)
}

// Advance converts sourceCycles into target cycles while preserving fractional
// remainder for later calls.
func (d *ClockDomain) Advance(sourceCycles uint32) uint32 {
	if d.sourceHz == 0 || d.targetHz == 0 || sourceCycles == 0 {
		return 0
	}

	total := uint64(sourceCycles)*d.targetHz + d.remainder
	cycles := total / d.sourceHz
	d.remainder = total % d.sourceHz
	return uint32(cycles)
}

// Reset clears any accumulated fractional remainder.
func (d *ClockDomain) Reset() {
	d.remainder = 0
}

// Remainder returns the current numerator remainder in source-clock units.
func (d *ClockDomain) Remainder() uint64 {
	return d.remainder
}

func clampPositive(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}
