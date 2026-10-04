package sndh

// MFP 68901 register offsets from mfpBase (odd addresses on the ST bus).
const (
	mfpGPIP  = 0x01
	mfpAER   = 0x03
	mfpDDR   = 0x05
	mfpIERA  = 0x07
	mfpIERB  = 0x09
	mfpIPRA  = 0x0b
	mfpIPRB  = 0x0d
	mfpISRA  = 0x0f
	mfpISRB  = 0x11
	mfpIMRA  = 0x13
	mfpIMRB  = 0x15
	mfpVR    = 0x17
	mfpTACR  = 0x19
	mfpTBCR  = 0x1b
	mfpTCDCR = 0x1d
	mfpTADR  = 0x1f
	mfpTBDR  = 0x21
	mfpTCDR  = 0x23
	mfpTDDR  = 0x25
)

// Interrupt channels of the four timers (bit numbers in IERA:IERB etc.).
const (
	chTimerD = 4
	chTimerC = 5
	chTimerB = 8
	chTimerA = 13
)

var timerChannel = [4]int{chTimerA, chTimerB, chTimerC, chTimerD}

// mfpPrescale maps a delay-mode control value (1..7) to its clock divider.
var mfpPrescale = [8]uint64{0, 4, 10, 16, 50, 64, 100, 200}

// mfpTimer is one MFP timer in delay mode. Time is counted in MFP clocks.
// Event-count mode is not modelled: such a timer simply does not run.
type mfpTimer struct {
	ctrl     byte   // 0..15 (A/B) or 0..7 (C/D)
	data     byte   // reload value, 0 = 256
	counter  uint64 // counter while stopped, 1..256
	next     uint64 // MFP time of the next timeout while running
	prescale uint64 // 0 while stopped
}

func reloadValue(v byte) uint64 {
	if v == 0 {
		return 256
	}
	return uint64(v)
}

// mfp emulates the parts of the MC68901 that music drivers use: the timers and
// the interrupt controller. It is driven by a monotonic MFP clock supplied by
// the machine.
type mfp struct {
	now    uint64
	timers [4]mfpTimer // A, B, C, D

	gpip, aer, ddr byte
	ier, ipr       uint16
	isr, imr       uint16
	vr             byte
	scratch        [0x30]byte // USART registers: stored, not emulated
}

func (m *mfp) reset() {
	*m = mfp{}
	for i := range m.timers {
		m.timers[i].counter = 256
	}
}

// nextEvent reports the MFP time of the earliest pending timeout, or ^0.
func (m *mfp) nextEvent() uint64 {
	next := ^uint64(0)
	for i := range m.timers {
		if t := &m.timers[i]; t.prescale != 0 && t.next < next {
			next = t.next
		}
	}
	return next
}

// advance runs the timers up to MFP time now, latching interrupts.
func (m *mfp) advance(now uint64) {
	if now <= m.now {
		return
	}
	m.now = now
	for i := range m.timers {
		t := &m.timers[i]
		if t.prescale == 0 || t.next > now {
			continue
		}
		period := t.prescale * reloadValue(t.data)
		n := (now-t.next)/period + 1
		t.next += n * period
		m.raise(timerChannel[i])
	}
}

func (m *mfp) raise(ch int) {
	bit := uint16(1) << ch
	if m.ier&bit != 0 {
		m.ipr |= bit
	}
}

// remaining reports a running timer's current counter value.
func (t *mfpTimer) remaining(now uint64) uint64 {
	if t.prescale == 0 {
		return t.counter
	}
	left := (t.next - now + t.prescale - 1) / t.prescale
	return min(max(left, 1), 256)
}

func (m *mfp) setControl(i int, ctrl byte) {
	t := &m.timers[i]
	t.ctrl = ctrl
	var prescale uint64
	if mode := ctrl & 0x0f; mode != 0 && mode != 8 {
		prescale = mfpPrescale[mode&7]
	}
	switch {
	case prescale == t.prescale:
	case prescale == 0: // stop: freeze the counter
		t.counter = t.remaining(m.now)
		t.prescale = 0
	case t.prescale == 0: // start from the frozen counter
		t.prescale = prescale
		t.next = m.now + prescale*max(t.counter, 1)
	default: // prescaler change while running
		left := t.remaining(m.now)
		t.prescale = prescale
		t.next = m.now + prescale*left
	}
}

func (m *mfp) setData(i int, v byte) {
	t := &m.timers[i]
	t.data = v
	if t.prescale == 0 {
		t.counter = reloadValue(v)
	}
}

// pending reports the highest-priority channel the MFP would present to the
// CPU, honouring the mask and, in software end-of-interrupt mode, the
// in-service register.
func (m *mfp) pending() (int, bool) {
	active := m.ipr & m.imr
	if active == 0 {
		return 0, false
	}
	ch := 15
	for active&(1<<ch) == 0 {
		ch--
	}
	if m.vr&0x08 != 0 && m.isr >= 1<<ch {
		return 0, false
	}
	return ch, true
}

// acknowledge is the CPU's interrupt acknowledge cycle for channel ch.
func (m *mfp) acknowledge(ch int) byte {
	bit := uint16(1) << ch
	m.ipr &^= bit
	if m.vr&0x08 != 0 {
		m.isr |= bit
	}
	return m.vr&0xf0 | byte(ch)
}

func (m *mfp) read(reg uint32) byte {
	switch reg {
	case mfpGPIP:
		// Inputs idle high: no ACIA/FDC interrupt, colour monitor.
		return m.gpip&m.ddr | ^m.ddr
	case mfpAER:
		return m.aer
	case mfpDDR:
		return m.ddr
	case mfpIERA:
		return byte(m.ier >> 8)
	case mfpIERB:
		return byte(m.ier)
	case mfpIPRA:
		return byte(m.ipr >> 8)
	case mfpIPRB:
		return byte(m.ipr)
	case mfpISRA:
		return byte(m.isr >> 8)
	case mfpISRB:
		return byte(m.isr)
	case mfpIMRA:
		return byte(m.imr >> 8)
	case mfpIMRB:
		return byte(m.imr)
	case mfpVR:
		return m.vr
	case mfpTACR:
		return m.timers[0].ctrl
	case mfpTBCR:
		return m.timers[1].ctrl
	case mfpTCDCR:
		return m.timers[2].ctrl<<4 | m.timers[3].ctrl
	case mfpTADR, mfpTBDR, mfpTCDR, mfpTDDR:
		return byte(m.timers[(reg-mfpTADR)/2].remaining(m.now))
	}
	if reg < uint32(len(m.scratch)) {
		return m.scratch[reg]
	}
	return 0xff
}

func (m *mfp) write(reg uint32, v byte) {
	switch reg {
	case mfpGPIP:
		m.gpip = v
	case mfpAER:
		m.aer = v
	case mfpDDR:
		m.ddr = v
	case mfpIERA, mfpIERB:
		shift := 8 * (mfpIERB - reg) / 2
		m.ier = m.ier&^(0xff<<shift) | uint16(v)<<shift
		m.ipr &= m.ier // disabling a channel drops its pending request
	case mfpIPRA, mfpIPRB: // writing 0 clears, 1 leaves unchanged
		shift := 8 * (mfpIPRB - reg) / 2
		m.ipr &= ^(uint16(^v) << shift)
	case mfpISRA, mfpISRB:
		shift := 8 * (mfpISRB - reg) / 2
		m.isr &= ^(uint16(^v) << shift)
	case mfpIMRA, mfpIMRB:
		shift := 8 * (mfpIMRB - reg) / 2
		m.imr = m.imr&^(0xff<<shift) | uint16(v)<<shift
	case mfpVR:
		m.vr = v
		if v&0x08 == 0 {
			m.isr = 0
		}
	case mfpTACR:
		m.setControl(0, v&0x0f)
	case mfpTBCR:
		m.setControl(1, v&0x0f)
	case mfpTCDCR:
		m.setControl(2, v>>4&7)
		m.setControl(3, v&7)
	case mfpTADR, mfpTBDR, mfpTCDR, mfpTDDR:
		m.setData(int(reg-mfpTADR)/2, v)
	default:
		if reg < uint32(len(m.scratch)) {
			m.scratch[reg] = v
		}
	}
}

// bestTimerSetting picks the delay-mode control value and data register that
// make a timer fire closest to hz.
func bestTimerSetting(hz int) (ctrl, data byte) {
	best := -1.0
	for c := 1; c <= 7; c++ {
		for d := 1; d <= 256; d++ {
			f := float64(mfpClock) / float64(mfpPrescale[c]*uint64(d))
			diff := f - float64(hz)
			if diff < 0 {
				diff = -diff
			}
			if best < 0 || diff < best {
				best, ctrl, data = diff, byte(c), byte(d)
			}
		}
	}
	return ctrl, data
}
