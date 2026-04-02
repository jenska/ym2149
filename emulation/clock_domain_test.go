package ym2149

import "testing"

func TestClockDomainOneToOne(t *testing.T) {
	domain := NewClockDomain(2_000_000, 2_000_000)

	if got := domain.Advance(1234); got != 1234 {
		t.Fatalf("Advance(1234) = %d, want 1234", got)
	}
	if got := domain.Remainder(); got != 0 {
		t.Fatalf("Remainder = %d, want 0", got)
	}
}

func TestClockDomainFractionalAccumulation(t *testing.T) {
	domain := NewClockDomain(8_000_000, 2_000_000)

	if got := domain.Advance(3); got != 0 {
		t.Fatalf("Advance(3) = %d, want 0", got)
	}
	if got := domain.Remainder(); got != 6_000_000 {
		t.Fatalf("Remainder after 3 = %d, want 6000000", got)
	}
	if got := domain.Advance(1); got != 1 {
		t.Fatalf("Advance(1) after remainder = %d, want 1", got)
	}
	if got := domain.Remainder(); got != 0 {
		t.Fatalf("Remainder after 4 total = %d, want 0", got)
	}
}

func TestClockDomainChunkingDeterminism(t *testing.T) {
	oneShot := NewClockDomain(8_021_247, 2_000_000)
	chunked := NewClockDomain(8_021_247, 2_000_000)

	want := oneShot.Advance(200_000)

	var got uint32
	for _, chunk := range []uint32{1, 7, 31, 4096, 128, 12_345, 183_392} {
		got += chunked.Advance(chunk)
	}

	if got != want {
		t.Fatalf("chunked cycles = %d, want %d", got, want)
	}
	if chunked.Remainder() != oneShot.Remainder() {
		t.Fatalf("chunked remainder = %d, want %d", chunked.Remainder(), oneShot.Remainder())
	}
}

func TestNewPSGClockDomain(t *testing.T) {
	domain := NewPSGClockDomain(8_000_000, 2_000_000)
	if got := domain.Advance(8); got != 2 {
		t.Fatalf("Advance(8) = %d, want 2", got)
	}
}
