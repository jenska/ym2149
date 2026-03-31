package psgdemo

import ym2149 "ym2149/emulation"

type ScriptStep struct {
	Name     string
	Duration int
	Apply    func(*ym2149.Chip)
}

type Sequencer struct {
	steps     []ScriptStep
	index     int
	remaining int
}

func NewSequencer(steps []ScriptStep) *Sequencer {
	return &Sequencer{steps: steps}
}

func (s *Sequencer) Reset(chip *ym2149.Chip) {
	s.index = 0
	if len(s.steps) == 0 {
		s.remaining = 0
		return
	}
	s.remaining = s.steps[0].Duration
	s.steps[0].Apply(chip)
}

func (s *Sequencer) Tick(chip *ym2149.Chip) {
	if len(s.steps) == 0 {
		return
	}
	if s.remaining <= 0 {
		s.index = (s.index + 1) % len(s.steps)
		s.remaining = s.steps[s.index].Duration
		s.steps[s.index].Apply(chip)
	}
	s.remaining--
}

func (s *Sequencer) CurrentName() string {
	if len(s.steps) == 0 {
		return "none"
	}
	return s.steps[s.index].Name
}

func DefaultSequence() []ScriptStep {
	return []ScriptStep{
		{
			Name:     "A4",
			Duration: 45,
			Apply: func(chip *ym2149.Chip) {
				ConfigureTone(chip, 440, 15, false, 0)
			},
		},
		{
			Name:     "C5",
			Duration: 45,
			Apply: func(chip *ym2149.Chip) {
				ConfigureTone(chip, 523.25, 15, false, 0)
			},
		},
		{
			Name:     "Envelope Sweep",
			Duration: 60,
			Apply: func(chip *ym2149.Chip) {
				ConfigureTone(chip, 659.25, 15, true, 0x0d)
				WriteReg(chip, 11, 0x04)
				WriteReg(chip, 12, 0x00)
			},
		},
		{
			Name:     "Noise Burst",
			Duration: 30,
			Apply: func(chip *ym2149.Chip) {
				ConfigureNoiseBurst(chip)
			},
		},
		{
			Name:     "Triangle Envelope",
			Duration: 60,
			Apply: func(chip *ym2149.Chip) {
				ConfigureTone(chip, 330, 12, true, 0x0e)
				WriteReg(chip, 11, 0x02)
				WriteReg(chip, 12, 0x00)
			},
		},
	}
}

func ConfigureTone(chip *ym2149.Chip, freq float64, volume byte, useEnvelope bool, shape byte) {
	period := TonePeriodForFrequency(chip.ClockHz(), freq)
	WriteReg(chip, 0, byte(period))
	WriteReg(chip, 1, byte(period>>8))
	WriteReg(chip, 6, 1)
	WriteReg(chip, 7, 0x3e)

	vol := volume & 0x0f
	if useEnvelope {
		vol |= 0x10
	}
	WriteReg(chip, 8, vol)
	WriteReg(chip, 9, 0)
	WriteReg(chip, 10, 0)
	if useEnvelope {
		WriteReg(chip, 13, shape&0x0f)
	}
}

func ConfigureNoiseBurst(chip *ym2149.Chip) {
	WriteReg(chip, 0, 0x20)
	WriteReg(chip, 1, 0)
	WriteReg(chip, 6, 0x04)
	WriteReg(chip, 7, 0x37)
	WriteReg(chip, 8, 0x10)
	WriteReg(chip, 11, 0x01)
	WriteReg(chip, 12, 0x00)
	WriteReg(chip, 13, 0x09)
}

func TonePeriodForFrequency(clockHz int, freq float64) uint16 {
	if freq <= 0 {
		return 1
	}
	period := int(float64(clockHz)/(16.0*freq) + 0.5)
	if period < 1 {
		period = 1
	}
	if period > 0x0fff {
		period = 0x0fff
	}
	return uint16(period)
}

func WriteReg(chip *ym2149.Chip, reg byte, value byte) {
	chip.SelectRegister(reg)
	chip.WriteData(value)
}
