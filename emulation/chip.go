// Package ym2149 implements a cycle-accurate YM2149 Programmable Sound Generator (PSG) emulator.
//
// The YM2149 is a 3-voice sound chip used in various retro computers and arcade machines,
// most notably the Atari ST. It provides three square wave tone channels, one noise channel,
// and a programmable envelope generator. Each channel can mix tone and noise with selectable
// volume levels or envelope control.
//
// The emulator maintains full compatibility with the YM2149F variant used in Atari ST computers,
// including proper envelope shapes, noise generation, and I/O port handling.
//
// Key features:
//   - Cycle-accurate emulation at any master clock frequency
//   - Real-time PCM audio output with configurable sample rates
//   - Thread-safe for concurrent audio generation and register access
//   - Bidirectional I/O ports with programmable direction
//   - All 16 envelope shapes supported
//
// Basic usage:
//
//	chip := ym2149.New(ym2149.Config{
//	    ClockHz:          2000000, // 2MHz master clock
//	    OutputSampleRate: 48000,   // 48kHz PCM output
//	    BufferSamples:    4096,    // Internal buffer size
//	})
//
//	// Select register and write data
//	chip.SelectRegister(0)  // Tone period low byte for channel A
//	chip.WriteData(0x20)    // Set period
//
//	// Advance emulation and get audio samples
//	chip.Step(1000)         // Advance 1000 master clock cycles
//	samples := make([]float32, chip.BufferedSamples())
//	chip.DrainMonoF32(samples) // Get mono PCM samples
package ym2149

import (
	"errors"
	"sync"
)

// Config controls the YM2149 chip clock and PCM output buffer settings.
// All fields must be positive values; zero or negative values will use defaults.
type Config struct {
	// ClockHz is the master clock frequency in Hz (typically 2000000 for Atari ST).
	// This determines the timing of all internal operations.
	ClockHz int

	// OutputSampleRate is the desired PCM sample rate in Hz for audio output.
	// Common values are 44100 or 48000.
	OutputSampleRate int

	// BufferSamples is the size of the internal PCM sample buffer.
	// Larger buffers reduce the chance of audio underruns but increase latency.
	BufferSamples int
}

// Validate checks that the configuration is valid and applies defaults for missing values.
func (cfg *Config) Validate() error {
	if cfg.ClockHz < 0 {
		return errors.New("ClockHz cannot be negative")
	}
	if cfg.OutputSampleRate < 0 {
		return errors.New("OutputSampleRate cannot be negative")
	}
	if cfg.BufferSamples < 0 {
		return errors.New("BufferSamples cannot be negative")
	}
	return nil
}

// Ports captures the current input latches and output drive state.
// The YM2149 has two 8-bit I/O ports (A and B) that can be configured as input or output.
type Ports struct {
	// AInput contains the current value latched from port A when configured as input.
	AInput byte
	// BInput contains the current value latched from port B when configured as input.
	BInput byte
	// AOutput contains the value being driven to port A when configured as output.
	AOutput byte
	// BOutput contains the value being driven to port B when configured as output.
	BOutput byte
}

// Chip is a reusable YM2149F-compatible PSG core.
//
// The chip maintains all internal state including registers, counters, and envelope generators.
// All methods are thread-safe and can be called concurrently from multiple goroutines.
type Chip struct {
	mu sync.Mutex

	cfg Config

	// YM2149 registers (R0-R15)
	// R0-R1: Channel A tone period (12-bit, little-endian)
	// R2-R3: Channel B tone period (12-bit, little-endian)
	// R4-R5: Channel C tone period (12-bit, little-endian)
	// R6: Noise period (5-bit)
	// R7: Mixer control and I/O port direction
	// R8-R10: Channel volumes (4-bit + envelope enable)
	// R11-R12: Envelope period (16-bit, little-endian)
	// R13: Envelope shape (4-bit)
	// R14: I/O port A data
	// R15: I/O port B data
	registers [16]byte

	// Currently selected register for read/write operations
	selected byte

	// Total master clock cycles executed
	cycles uint64

	// Current I/O port state
	ports Ports

	// Internal timing state - runs at ClockHz/8
	internalDividerPhase uint8

	// Tone generator state for each channel
	toneCounters [3]uint16 // Current counter values
	toneOutputs  [3]bool   // Current output levels (true = high)

	// Noise generator state
	noiseCounter uint8  // Current counter value
	noiseOutput  bool   // Current output level
	noiseLFSR    uint32 // 17-bit LFSR for noise generation

	// Envelope generator state
	envCounter   uint16 // Current counter value
	envStep      int    // Current step in envelope (0-31)
	envVolume    int    // Current envelope volume level
	envAttack    int    // Current attack direction (0 or 31)
	envHold      bool   // Whether envelope is in hold phase
	envAlternate bool   // Whether envelope alternates
	envHolding   bool   // Whether envelope is currently holding

	// PCM sample generation state
	samplePhase uint64     // Current sample phase accumulator
	sampleAccum float64    // Accumulated sample value
	samples     ringBuffer // Output sample buffer
}

type ringBuffer struct {
	data  []float32
	read  int
	write int
	count int
}

// New constructs a chip with sensible Atari ST defaults.
func New(cfg Config) *Chip {
	cfg = cfg.withDefaults()
	c := &Chip{
		cfg: cfg,
		samples: ringBuffer{
			data: make([]float32, cfg.BufferSamples),
		},
	}
	c.resetLocked()
	return c
}

// Reset restores the chip to power-on state.
func (c *Chip) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetLocked()
}

// Step advances the chip by the given number of master clock cycles.
func (c *Chip) Step(cycles uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := uint32(0); i < cycles; i++ {
		c.integrateCycleLocked(c.mixLevelLocked())
		c.cycles++
		c.internalDividerPhase++
		if c.internalDividerPhase == internalDivider {
			c.internalDividerPhase = 0
			c.tickInternalLocked()
		}
	}
}

// Cycles returns the total number of master clock cycles executed.
func (c *Chip) Cycles() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cycles
}

// ClockHz reports the configured master clock.
func (c *Chip) ClockHz() int {
	return c.cfg.ClockHz
}

// OutputSampleRate reports the configured PCM sample rate.
func (c *Chip) OutputSampleRate() int {
	return c.cfg.OutputSampleRate
}

// BufferedSamples reports how many mono PCM samples are currently queued.
func (c *Chip) BufferedSamples() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.samples.count
}

// SelectRegister latches the active PSG register.
func (c *Chip) SelectRegister(reg byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.selected = reg & 0x0f
}

// WriteData writes to the currently selected register.
func (c *Chip) WriteData(v byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeSelectedLocked(v)
}

// ReadData reads from the currently selected register.
func (c *Chip) ReadData() byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readSelectedLocked()
}

// SetPortAInput updates the current port A input latch.
func (c *Chip) SetPortAInput(v byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ports.AInput = v
}

// SetPortBInput updates the current port B input latch.
func (c *Chip) SetPortBInput(v byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ports.BInput = v
}

// Ports returns a snapshot of the current port state.
func (c *Chip) Ports() Ports {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ports
}

// DrainMonoF32 copies queued mono PCM samples into dst.
func (c *Chip) DrainMonoF32(dst []float32) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.samples.pop(dst)
}

func (cfg Config) withDefaults() Config {
	if cfg.ClockHz <= 0 {
		cfg.ClockHz = defaultClockHz
	}
	if cfg.OutputSampleRate <= 0 {
		cfg.OutputSampleRate = defaultOutputSampleRate
	}
	if cfg.BufferSamples <= 0 {
		cfg.BufferSamples = defaultBufferSamples
	}
	return cfg
}

func (c *Chip) resetLocked() {
	c.registers = [16]byte{}
	c.selected = 0
	c.cycles = 0
	c.ports = Ports{}
	c.internalDividerPhase = 0
	c.toneCounters = [3]uint16{}
	c.toneOutputs = [3]bool{true, true, true}
	c.noiseCounter = 0
	c.noiseOutput = true
	c.noiseLFSR = 0x1ffff
	c.envCounter = 0
	c.envStep = envelopeSteps - 1
	c.envVolume = 0
	c.envAttack = 0
	c.envHold = true
	c.envAlternate = false
	c.envHolding = false
	c.samplePhase = 0
	c.sampleAccum = 0
	c.samples.reset()
	c.reloadEnvelopeLocked(0)
	c.updatePortsLocked()
}

func (c *Chip) writeSelectedLocked(v byte) {
	reg := c.selected & 0x0f
	c.registers[reg] = v

	switch reg {
	case 7, 14, 15:
		c.updatePortsLocked()
	case 13:
		c.reloadEnvelopeLocked(v & 0x0f)
	}
}

func (c *Chip) readSelectedLocked() byte {
	switch c.selected & 0x0f {
	case 14:
		if c.portAIsInputLocked() {
			return c.ports.AInput
		}
	case 15:
		if c.portBIsInputLocked() {
			return c.ports.BInput
		}
	}
	return c.registers[c.selected&0x0f]
}

func (c *Chip) updatePortsLocked() {
	if c.portAIsInputLocked() {
		c.ports.AOutput = 0
	} else {
		c.ports.AOutput = c.registers[14]
	}
	if c.portBIsInputLocked() {
		c.ports.BOutput = 0
	} else {
		c.ports.BOutput = c.registers[15]
	}
}

func (c *Chip) portAIsInputLocked() bool {
	return c.registers[7]&0x40 != 0
}

func (c *Chip) portBIsInputLocked() bool {
	return c.registers[7]&0x80 != 0
}

func (c *Chip) tickInternalLocked() {
	for ch := 0; ch < 3; ch++ {
		c.toneCounters[ch]++
		if c.toneCounters[ch] >= c.tonePeriodLocked(ch) {
			c.toneCounters[ch] = 0
			c.toneOutputs[ch] = !c.toneOutputs[ch]
		}
	}

	c.noiseCounter++
	if c.noiseCounter >= c.noisePeriodLocked() {
		c.noiseCounter = 0
		c.advanceNoiseLocked()
	}

	c.envCounter++
	if c.envCounter >= c.envelopePeriodLocked() {
		c.envCounter = 0
		c.advanceEnvelopeLocked()
	}
}

func (c *Chip) tonePeriodLocked(ch int) uint16 {
	fine := uint16(c.registers[ch*2])
	coarse := uint16(c.registers[ch*2+1] & 0x0f)
	period := (coarse << 8) | fine
	if period == 0 {
		return 1
	}
	return period
}

func (c *Chip) noisePeriodLocked() uint8 {
	period := c.registers[6] & 0x1f
	if period == 0 {
		return 1
	}
	return period
}

func (c *Chip) envelopePeriodLocked() uint16 {
	period := uint16(c.registers[11]) | (uint16(c.registers[12]) << 8)
	if period == 0 {
		return 1
	}
	return period
}

func (c *Chip) advanceNoiseLocked() {
	feedback := (c.noiseLFSR ^ (c.noiseLFSR >> 3)) & 0x1
	c.noiseLFSR = (c.noiseLFSR >> 1) | (feedback << 16)
	c.noiseOutput = c.noiseLFSR&0x1 != 0
}

func (c *Chip) reloadEnvelopeLocked(shape byte) {
	c.envAttack = 0
	if shape&0x04 != 0 {
		c.envAttack = envelopeSteps - 1
	}
	c.envHold = shape&0x01 != 0
	c.envAlternate = shape&0x02 != 0
	if shape&0x08 == 0 {
		c.envHold = true
		c.envAlternate = c.envAttack != 0
	}
	c.envHolding = false
	c.envCounter = 0
	c.envStep = envelopeSteps - 1
	c.envVolume = c.envStep ^ c.envAttack
}

func (c *Chip) advanceEnvelopeLocked() {
	if c.envHolding {
		return
	}

	c.envStep--
	if c.envStep < 0 {
		if c.envHold {
			if c.envAlternate {
				c.envAttack ^= envelopeSteps - 1
			}
			c.envHolding = true
			c.envStep = 0
		} else {
			if c.envAlternate {
				c.envAttack ^= envelopeSteps - 1
			}
			c.envStep &= envelopeSteps - 1
		}
	}

	c.envVolume = c.envStep ^ c.envAttack
}

func (c *Chip) mixLevelLocked() float64 {
	mixer := c.registers[7]
	envMask := 0
	levels := [3]int{}

	for ch := 0; ch < 3; ch++ {
		toneDisabled := mixer&(1<<ch) != 0
		noiseDisabled := mixer&(1<<(ch+3)) != 0
		tonePass := toneDisabled || c.toneOutputs[ch]
		noisePass := noiseDisabled || c.noiseOutput
		if !tonePass || !noisePass {
			continue
		}

		reg := c.registers[8+ch] & 0x1f
		if reg&0x10 != 0 {
			envMask |= 1 << ch
			levels[ch] = c.envVolume
			continue
		}
		levels[ch] = int(reg & 0x0f)
	}

	return float64(ym2149AnalogMixLevels[analogMixIndex(envMask, levels)])
}

func (c *Chip) integrateCycleLocked(level float64) {
	nextPhase := c.samplePhase + uint64(c.cfg.OutputSampleRate)
	if nextPhase < uint64(c.cfg.ClockHz) {
		c.sampleAccum += level * float64(c.cfg.OutputSampleRate)
		c.samplePhase = nextPhase
		return
	}

	firstWeight := uint64(c.cfg.ClockHz) - c.samplePhase
	c.sampleAccum += level * float64(firstWeight)
	c.samples.push(float32(c.sampleAccum / float64(c.cfg.ClockHz)))

	overflow := nextPhase - uint64(c.cfg.ClockHz)
	c.sampleAccum = level * float64(overflow)
	c.samplePhase = overflow
}

func (r *ringBuffer) reset() {
	for i := range r.data {
		r.data[i] = 0
	}
	r.read = 0
	r.write = 0
	r.count = 0
}

func (r *ringBuffer) push(v float32) {
	if len(r.data) == 0 {
		return
	}
	if r.count == len(r.data) {
		r.data[r.write] = v
		r.write = (r.write + 1) % len(r.data)
		r.read = r.write
		return
	}

	r.data[r.write] = v
	r.write = (r.write + 1) % len(r.data)
	r.count++
}

func (r *ringBuffer) pop(dst []float32) int {
	if len(dst) == 0 || r.count == 0 {
		return 0
	}
	n := len(dst)
	if n > r.count {
		n = r.count
	}
	for i := 0; i < n; i++ {
		dst[i] = r.data[r.read]
		r.read = (r.read + 1) % len(r.data)
	}
	r.count -= n
	return n
}
