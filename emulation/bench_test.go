package ym2149

import (
	"testing"

	"github.com/jenska/ym2149/renderer/ebitenaudio"
)

func BenchmarkStep(b *testing.B) {
	chip := configuredBenchmarkChip()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chip.Step(33_333)
	}
}

func BenchmarkDrainMonoF32(b *testing.B) {
	chip := configuredBenchmarkChip()
	buf := make([]float32, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chip.Step(50_000)
		chip.DrainMonoF32(buf)
	}
}

func BenchmarkAudioPipeline(b *testing.B) {
	chip := configuredBenchmarkChip()
	reader := ebitenaudio.NewReader(chip, 1024)
	pcm := make([]byte, 1024*8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chip.Step(50_000)
		if _, err := reader.Read(pcm); err != nil {
			b.Fatalf("Read: %v", err)
		}
	}
}

func configuredBenchmarkChip() *Chip {
	chip := New(Config{})
	chip.SelectRegister(0)
	chip.WriteData(0x20)
	chip.SelectRegister(1)
	chip.WriteData(0x01)
	chip.SelectRegister(6)
	chip.WriteData(0x03)
	chip.SelectRegister(7)
	chip.WriteData(0x30)
	chip.SelectRegister(8)
	chip.WriteData(0x10)
	chip.SelectRegister(9)
	chip.WriteData(0x0c)
	chip.SelectRegister(10)
	chip.WriteData(0x08)
	chip.SelectRegister(11)
	chip.WriteData(0x02)
	chip.SelectRegister(12)
	chip.WriteData(0x00)
	chip.SelectRegister(13)
	chip.WriteData(0x0d)
	return chip
}
