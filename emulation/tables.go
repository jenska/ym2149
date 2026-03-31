package ym2149

import "math"

const (
	defaultClockHz           = 2_000_000
	defaultOutputSampleRate  = 48_000
	defaultBufferSamples     = 4_096
	internalDivider          = 8
	envelopeSteps            = 32
	analogLoadResistance     = 1_000.0
	analogPullUpResistance   = 630.0
	analogPullDownResistance = 801.0
	analogMixTableSize       = 8 * 32 * 32 * 32
)

var ym2149FixedResistors = [16]float64{
	73770, 37586, 27458, 21451, 15864, 12371, 8922, 6796,
	4763, 3521, 2403, 1737, 1123, 762, 438, 251,
}

var ym2149EnvelopeResistors = [envelopeSteps]float64{
	103350, 73770, 52657, 37586, 32125, 27458, 24269, 21451,
	18447, 15864, 14009, 12371, 10506, 8922, 7787, 6796,
	5689, 4763, 4095, 3521, 2909, 2403, 2043, 1737,
	1397, 1123, 925, 762, 578, 438, 332, 251,
}

var ym2149AnalogMixLevels = buildAnalogMixTable()

func buildAnalogMixTable() []float32 {
	raw := make([]float64, analogMixTableSize)
	maxRaw := 0.0

	for envMask := 0; envMask < 8; envMask++ {
		ch1Levels, ch2Levels, ch3Levels := ym2149FixedResistors[:], ym2149FixedResistors[:], ym2149FixedResistors[:]
		if envMask&0x01 != 0 {
			ch1Levels = ym2149EnvelopeResistors[:]
		}
		if envMask&0x02 != 0 {
			ch2Levels = ym2149EnvelopeResistors[:]
		}
		if envMask&0x04 != 0 {
			ch3Levels = ym2149EnvelopeResistors[:]
		}

		for ch1 := 0; ch1 < len(ch1Levels); ch1++ {
			for ch2 := 0; ch2 < len(ch2Levels); ch2++ {
				for ch3 := 0; ch3 < len(ch3Levels); ch3++ {
					rawValue := analogLevel(ch1Levels[ch1], ch2Levels[ch2], ch3Levels[ch3])
					if rawValue > maxRaw {
						maxRaw = rawValue
					}
					raw[(envMask<<15)|(ch3<<10)|(ch2<<5)|ch1] = rawValue
				}
			}
		}
	}

	silence := raw[0]
	scale := maxRaw - silence
	if scale <= 0 {
		scale = 1
	}

	mixed := make([]float32, analogMixTableSize)
	for i, value := range raw {
		normalized := (value - silence) / scale
		if normalized < 0 {
			normalized = 0
		}
		mixed[i] = float32(normalized)
	}
	return mixed
}

func analogLevel(ch1, ch2, ch3 float64) float64 {
	rt := 3.0/analogPullUpResistance + 3.0/analogPullDownResistance + 1.0/analogLoadResistance
	rw := 3.0 / analogPullUpResistance

	rt += 1.0/ch1 + 1.0/ch2 + 1.0/ch3
	rw += 1.0/ch1 + 1.0/ch2 + 1.0/ch3
	return rw / rt
}

func analogMixIndex(envMask int, levels [3]int) int {
	return (envMask << 15) | (levels[2] << 10) | (levels[1] << 5) | levels[0]
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
