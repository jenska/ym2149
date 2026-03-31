package ym2149

import (
	"slices"
	"testing"
)

func TestEnvelopeShapes(t *testing.T) {
	expected := map[byte][]int{
		0x0: append(descending(), hold(0, 32)...),
		0x1: append(descending(), hold(0, 32)...),
		0x2: append(descending(), hold(0, 32)...),
		0x3: append(descending(), hold(0, 32)...),
		0x4: append(ascending(), hold(0, 32)...),
		0x5: append(ascending(), hold(0, 32)...),
		0x6: append(ascending(), hold(0, 32)...),
		0x7: append(ascending(), hold(0, 32)...),
		0x8: append(descending(), descending()...),
		0x9: append(descending(), hold(0, 32)...),
		0xa: append(descending(), ascending()...),
		0xb: append(descending(), hold(31, 32)...),
		0xc: append(ascending(), ascending()...),
		0xd: append(ascending(), hold(31, 32)...),
		0xe: append(ascending(), descending()...),
		0xf: append(ascending(), hold(0, 32)...),
	}

	for shape, want := range expected {
		t.Run(string(rune('A'+shape)), func(t *testing.T) {
			chip := New(Config{})
			chip.SelectRegister(11)
			chip.WriteData(1)
			chip.SelectRegister(12)
			chip.WriteData(0)
			chip.SelectRegister(13)
			chip.WriteData(shape)

			got := []int{chip.envVolume}
			for len(got) < len(want) {
				chip.Step(8)
				got = append(got, chip.envVolume)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("shape 0x%x\n got: %v\nwant: %v", shape, got, want)
			}
		})
	}
}

func descending() []int {
	out := make([]int, envelopeSteps)
	for i := range out {
		out[i] = envelopeSteps - 1 - i
	}
	return out
}

func ascending() []int {
	out := make([]int, envelopeSteps)
	for i := range out {
		out[i] = i
	}
	return out
}

func hold(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}
