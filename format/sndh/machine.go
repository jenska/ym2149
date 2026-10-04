package sndh

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/jenska/m68kasm"
	"github.com/jenska/m68kemu"
	ym2149 "github.com/jenska/ym2149/emulation"
)

// PAL Atari ST clocks. The CPU runs at exactly four times the PSG clock, so a
// CPU cycle count maps onto PSG cycles with a shift.
const (
	cpuClock  = 8_021_248
	psgClock  = cpuClock / 4
	mfpClock  = 2_457_600
	vblCycles = 313 * 512 // one PAL frame
)

// Memory map.
const (
	ramSize    = 4 << 20
	tuneBase   = 0x10000
	sspTop     = 0x8000
	uspTop     = 0xf000
	heapBase   = 0x200000
	screenBase = 0x3f8000

	romBase  = 0xfc0000
	romEnd   = 0xfcffff
	varsBase = 0xfce000
	varsSize = 0x1000
	hostBase = 0xfcf000
	hostSize = 0x20

	ioBase  = 0xff8000
	psgBase = 0xff8800
	psgEnd  = 0xff88ff
	mfpBase = 0xfffa00
	mfpEnd  = 0xfffa3f
)

// Host registers (offsets from hostBase) and TOS stub variables (from
// varsBase). tos.asm receives the absolute addresses as symbols.
const (
	hArgs    = 0x00
	hFrame   = 0x04
	hCall    = 0x08
	hResult  = 0x0c
	hEvent   = 0x10
	hSubtune = 0x14

	vAcc     = 0x00
	vRate    = 0x02
	vBusy    = 0x04
	vVBLPlay = 0x05
	vTick4   = 0x06
	vISR     = 0x08
	vISRClr  = 0x0c
	vKbdv    = 0x100 // Kbdvbase table
	vIorec   = 0x200 // Iorec buffer records
	vKeytbl  = 0x280 // Keytbl pointers
	vCookies = 0x300 // cookie jar

	evInit  = 1
	evCrash = 2
)

//go:embed tos.asm
var tosSource string

type tosImage struct {
	code []byte
	addr map[string]uint32
}

var (
	tosOnce sync.Once
	tos     tosImage
	tosErr  error
)

// loadTOS assembles the TOS stand-in once.
func loadTOS() (*tosImage, error) {
	tosOnce.Do(func() {
		syms := map[string]uint32{
			"TUNE":      tuneBase,
			"SSP_TOP":   sspTop,
			"USP_TOP":   uspTop,
			"H_ARGS":    hostBase + hArgs,
			"H_FRAME":   hostBase + hFrame,
			"H_CALL":    hostBase + hCall,
			"H_RESULT":  hostBase + hResult,
			"H_EVENT":   hostBase + hEvent,
			"H_SUBTUNE": hostBase + hSubtune,
			"V_ACC":     varsBase + vAcc,
			"V_RATE":    varsBase + vRate,
			"V_BUSY":    varsBase + vBusy,
			"V_VBLPLAY": varsBase + vVBLPlay,
			"V_TICK4":   varsBase + vTick4,
			"V_ISR":     varsBase + vISR,
			"V_ISRCLR":  varsBase + vISRClr,
			"EV_INIT":   evInit,
			"EV_CRASH":  evCrash,
		}
		res, err := m68kasm.AssembleStringDetailedWithOptions(tosSource, m68kasm.ParseOptions{Symbols: syms})
		if err != nil {
			tosErr = fmt.Errorf("sndh: assembling TOS stub: %w", err)
			return
		}
		if res.Origin != romBase || len(res.Bytes) > varsBase-romBase {
			tosErr = fmt.Errorf("sndh: TOS stub at %#x, %d bytes, does not fit the ROM window", res.Origin, len(res.Bytes))
			return
		}
		tos = tosImage{code: res.Bytes, addr: res.Labels}
	})
	return &tos, tosErr
}

func (t *tosImage) label(name string) uint32 {
	a, ok := t.addr[name]
	if !ok {
		panic("sndh: TOS stub lacks label " + name)
	}
	return a
}

// machine is a minimal Atari ST: RAM, the TOS stub, PSG, MFP and VBL.
type machine struct {
	cpu  m68kemu.CPU
	bus  *m68kemu.Bus
	tos  *tosImage
	file *File

	ram  []byte
	vars [varsSize]byte
	host [hostSize]byte
	io   [0x8000]byte // other I/O registers: stored, not emulated

	chip      *ym2149.Chip
	psgSel    byte
	psgReg    [16]byte // register file as the CPU sees it (writes are queued)
	psgOffset uint64   // CPU/4 cycles minus PSG cycles: time skipped by Seek
	skipping  bool     // Seek in progress: keep PSG writes in psgReg only

	mfp        mfp
	nextSync   uint64 // CPU cycle of the next timer or VBL event
	nextVBL    uint64
	vblPending bool

	heap      uint32
	rand      uint32
	playStart uint64 // CPU cycle INIT returned at; 0 while INIT runs
	crashed   bool
	crash     string
}

func newMachine(f *File, chip *ym2149.Chip) (*machine, error) {
	t, err := loadTOS()
	if err != nil {
		return nil, err
	}
	if tuneBase+len(f.Data) > heapBase {
		return nil, fmt.Errorf("sndh: image of %d bytes is too large", len(f.Data))
	}
	m := &machine{tos: t, file: f, chip: chip, ram: make([]byte, ramSize)}
	m.bus = m68kemu.NewBus(
		ramDevice{m},
		romDevice{m},
		ioDevice{m},
	)
	cpu, err := m68kemu.NewCPU(m.bus, m68kemu.WithDeferredReset(), m68kemu.WithCycleRounding(4))
	if err != nil {
		return nil, err
	}
	m.cpu = cpu
	cpu.SetFastMemory(m68kemu.FastRegion{Base: 0, Mem: m.ram})
	cpu.SetIRQSource(m)
	return m, nil
}

// boot loads the image, sets up TOS state and starts INIT for subtune.
func (m *machine) boot(subtune int) error {
	clear(m.ram)
	clear(m.vars[:])
	clear(m.host[:])
	clear(m.io[:])
	copy(m.ram[tuneBase:], m.file.Data)
	m.heap = heapBase
	m.rand = 0x2a3b4c5d
	m.playStart = 0
	m.crashed, m.crash = false, ""

	t := m.tos
	crash, rte := t.label("crash"), t.label("nop_rte")
	for v := uint32(2); v < 256; v++ {
		h := rte
		if v < 24 {
			h = crash
		}
		m.poke32(v*4, h)
	}
	m.poke32(26*4, t.label("hbl"))
	m.poke32(28*4, t.label("vbl"))
	m.poke32(33*4, t.label("trap_gemdos"))
	m.poke32(45*4, t.label("trap_bios"))
	m.poke32(46*4, t.label("trap_xbios"))
	m.poke32(mfpVector(chTimerC), t.label("timer_c"))
	m.poke32(0, sspTop)
	m.poke32(4, t.label("reset"))
	// Reset the CPU first: device timing below is based on its cycle count.
	if err := m.cpu.Reset(); err != nil {
		return err
	}

	// System variables.
	rts := t.label("nop_rts")
	m.poke32(0x400, rts) // etv_timer
	m.poke32(0x404, rts) // etv_critic
	m.poke32(0x408, rts) // etv_term
	m.poke32(0x420, 0x752019f3)
	m.poke32(0x42e, ramSize)    // phystop
	m.poke32(0x432, heapBase)   // _membot
	m.poke32(0x436, screenBase) // _memtop
	m.poke32(0x43a, 0x237698aa)
	m.poke16(0x442, 20) // _timr_ms
	m.poke32(0x44e, screenBase)
	m.poke16(0x452, 1) // vblsem
	m.poke16(0x454, 8) // nvbls
	m.poke32(0x456, 0x4ce)
	m.poke32(0x4f2, romBase) // _sysbase
	m.poke32(0x5a0, varsBase+vCookies)

	for i := range uint32(9) {
		m.putVar32(vKbdv+4*i, rts)
	}
	m.putVar32(vCookies, 0x5f4d4348) // _MCH: ST
	m.putVar32(vCookies+4, 0)
	m.putVar32(vCookies+8, 0x5f534e44) // _SND: PSG only
	m.putVar32(vCookies+12, 1)
	m.putVar32(vCookies+16, 0)
	m.putVar32(vCookies+20, 8)
	binary.BigEndian.PutUint16(m.vars[vTick4:], 4)
	binary.BigEndian.PutUint16(m.host[hSubtune:], uint16(subtune))

	m.io[0x020a] = 0x02 // 50 Hz

	// MFP as TOS leaves it: software end-of-interrupt, vectors at $100, and
	// Timer C running at 200 Hz.
	m.mfp.reset()
	m.nextSync = 0
	m.nextVBL = vblCycles
	m.vblPending = false
	m.mfpWrite(mfpVR, 0x48)
	m.mfpWrite(mfpTCDR, 192)
	m.mfpWrite(mfpTCDCR, 0x50) // prescale 64
	m.enableMFPChannel(chTimerC, true)

	m.chip.Reset()
	m.psgSel = 0
	m.psgOffset = 0
	m.skipping = false
	clear(m.psgReg[:])
	m.psgWrite(7, 0xff)
	return nil
}

// arm starts calling PLAY once INIT has returned.
func (m *machine) arm() {
	m.playStart = max(m.cpu.Cycles(), 1)
	r := m.file.Replay
	switch r.Timer {
	case TimerVBL:
		m.vars[vVBLPlay] = 1
	case TimerC:
		if r.Hz <= 200 && 200%r.Hz == 0 {
			binary.BigEndian.PutUint16(m.vars[vRate:], uint16(r.Hz))
			return
		}
		// Not a divisor of the 200 Hz tick: run Timer C at the replay rate
		// itself and play on every tick.
		ctrl, data := bestTimerSetting(r.Hz)
		m.mfpWrite(mfpTCDCR, m.mfp.read(mfpTCDCR)&0x0f)
		m.mfpWrite(mfpTCDR, data)
		m.mfpWrite(mfpTCDCR, m.mfp.read(mfpTCDCR)&0x0f|ctrl<<4)
		binary.BigEndian.PutUint16(m.vars[vRate:], 200)
	default:
		timer := map[TimerKind]int{TimerA: 0, TimerB: 1, TimerD: 3}[r.Timer]
		ch := timerChannel[timer]
		isr := uint32(mfpBase + mfpISRB)
		if ch >= 8 {
			isr = mfpBase + mfpISRA
		}
		m.putVar32(vISR, isr)
		m.vars[vISRClr] = ^byte(1 << (ch & 7))
		ctrl, data := bestTimerSetting(r.Hz)
		m.xbtimer(timer, ctrl, data, m.tos.label("timer_play"))
	}
}

func mfpVector(ch int) uint32 { return 0x100 + 4*uint32(ch) }

// --- timing and interrupts ---

func cpuToMFP(c uint64) uint64 { return c * mfpClock / cpuClock }

// sync brings the MFP and VBL up to the CPU's current cycle when an event is
// due.
func (m *machine) sync() {
	if m.cpu.Cycles() >= m.nextSync {
		m.syncNow()
	}
}

func (m *machine) syncNow() {
	now := m.cpu.Cycles()
	if now >= m.nextVBL {
		m.vblPending = true
		m.nextVBL += ((now-m.nextVBL)/vblCycles + 1) * vblCycles
	}
	m.mfp.advance(cpuToMFP(now))
	m.reschedule()
}

func (m *machine) reschedule() {
	m.nextSync = m.nextVBL
	if t := m.mfp.nextEvent(); t != ^uint64(0) {
		// First CPU cycle whose MFP time reaches t.
		c := (t*cpuClock + mfpClock - 1) / mfpClock
		m.nextSync = min(m.nextSync, c)
	}
}

// PendingIRQ implements m68kemu.IRQSource: MFP on level 6, VBL on level 4.
func (m *machine) PendingIRQ() (level, vector uint8) {
	m.sync()
	if ch, ok := m.mfp.pending(); ok {
		return 6, m.mfp.vr&0xf0 | byte(ch)
	}
	if m.vblPending {
		return 4, m68kemu.AutoVector
	}
	return 0, 0
}

// AckIRQ implements m68kemu.IRQSource.
func (m *machine) AckIRQ(level uint8) {
	switch level {
	case 6:
		if ch, ok := m.mfp.pending(); ok {
			m.mfp.acknowledge(ch)
		}
	case 4:
		m.vblPending = false
	}
}

func (m *machine) mfpRead(reg uint32) byte {
	m.syncNow()
	return m.mfp.read(reg)
}

func (m *machine) mfpWrite(reg uint32, v byte) {
	m.syncNow()
	m.mfp.write(reg, v)
	m.reschedule()
}

func (m *machine) enableMFPChannel(ch int, on bool) {
	ierReg, imrReg := uint32(mfpIERB), uint32(mfpIMRB)
	if ch >= 8 {
		ierReg, imrReg = mfpIERA, mfpIMRA
	}
	bit := byte(1 << (ch & 7))
	for _, reg := range []uint32{ierReg, imrReg} {
		v := m.mfp.read(reg)
		if on {
			v |= bit
		} else {
			v &^= bit
		}
		m.mfpWrite(reg, v)
	}
}

// xbtimer is XBIOS Xbtimer: program timer 0..3 (A..D) and install vec.
func (m *machine) xbtimer(timer int, ctrl, data byte, vec uint32) {
	ch := timerChannel[timer]
	m.enableMFPChannel(ch, false)
	switch timer {
	case 0, 1:
		reg := uint32(mfpTACR + 2*timer)
		m.mfpWrite(reg, 0)
		m.mfpWrite(uint32(mfpTADR+2*timer), data)
		m.mfpWrite(reg, ctrl)
	case 2:
		low := m.mfp.read(mfpTCDCR) & 0x07
		m.mfpWrite(mfpTCDCR, low)
		m.mfpWrite(mfpTCDR, data)
		m.mfpWrite(mfpTCDCR, low|(ctrl&7)<<4)
	case 3:
		high := m.mfp.read(mfpTCDCR) & 0x70
		m.mfpWrite(mfpTCDCR, high)
		m.mfpWrite(mfpTDDR, data)
		m.mfpWrite(mfpTCDCR, high|ctrl&7)
	}
	if vec != 0 {
		m.poke32(mfpVector(ch), vec)
	}
	m.enableMFPChannel(ch, true)
}

// --- PSG ---

// psgCycle is the PSG clock cycle matching the CPU's current cycle.
func (m *machine) psgCycle() uint64 { return m.cpu.Cycles()/4 - m.psgOffset }

func (m *machine) psgWrite(reg, v byte) {
	reg &= 0x0f
	m.psgReg[reg] = v
	if !m.skipping {
		m.chip.WriteAt(m.psgCycle(), reg, v)
	}
}

// resyncPSG ends a silent skip: the chip takes over the register file the
// driver left and continues from the CPU's current time.
func (m *machine) resyncPSG() {
	m.skipping = false
	m.chip.ClearPendingWrites()
	for r := range byte(14) {
		m.chip.Write(r, m.psgReg[r])
	}
	m.psgOffset = m.cpu.Cycles()/4 - m.chip.Cycles()
}

func (m *machine) psgReadByte(addr uint32) byte {
	if addr&1 != 0 {
		return 0xff
	}
	return m.psgReg[m.psgSel]
}

func (m *machine) psgWriteByte(addr uint32, v byte) {
	switch addr & 3 {
	case 0:
		m.psgSel = v & 0x0f
	case 2:
		m.psgWrite(m.psgSel, v)
	}
}

// --- host services ---

func (m *machine) hostWrite(off uint32, size m68kemu.Size, v uint32) {
	putBytes(m.host[:], off, size, v)
	if size != m68kemu.Word {
		return
	}
	switch off {
	case hCall:
		r := m.trap(uint16(v), binary.BigEndian.Uint32(m.host[hArgs:]), binary.BigEndian.Uint32(m.host[hFrame:]))
		binary.BigEndian.PutUint32(m.host[hResult:], r)
	case hEvent:
		switch v {
		case evInit:
			m.arm()
		case evCrash:
			m.crashed = true
			st := m.cpu.DebugState()
			m.crash = fmt.Sprintf("exception vector %d at pc %06x", st.LastException.Vector, st.LastException.PC)
		}
	}
}

const errInvalidFunction = 0xffffffe0 // EINVFN

// trap performs a GEMDOS (1), BIOS (13) or XBIOS (14) call whose arguments
// start at args, the function number first. frame points at the saved d1/a0
// in front of the exception frame.
func (m *machine) trap(n uint16, args, frame uint32) uint32 {
	fn := m.peek16(args)
	w := func(i uint32) uint16 { return m.peek16(args + i) }
	l := func(i uint32) uint32 { return m.peek32(args + i) }

	switch n {
	case 1: // GEMDOS
		switch fn {
		case 0x20: // Super(stack)
			sr := m.peek16(frame + 8)
			if l(2) == 1 {
				if sr&0x2000 != 0 {
					return 0xffffffff
				}
				return 0
			}
			if sr&0x2000 == 0 {
				m.poke16(frame+8, sr|0x2000)
			}
			return frame + 14
		case 0x30: // Sversion
			return 0x1500
		case 0x48: // Malloc(size)
			size := l(2)
			if size == 0xffffffff {
				return screenBase - m.heap
			}
			size = (size + 1) &^ 1
			if size > screenBase-m.heap {
				return 0
			}
			p := m.heap
			m.heap += size
			return p
		case 0x49, 0x4a: // Mfree, Mshrink
			return 0
		case 0x00, 0x02, 0x07, 0x08, 0x09, 0x0b, 0x2a, 0x2c, 0x31, 0x4c:
			return 0 // console, time and termination: nothing to do
		}
		return errInvalidFunction
	case 13: // BIOS
		switch fn {
		case 5: // Setexc(vec, addr)
			vec := uint32(w(2)) * 4
			old := m.peek32(vec)
			if addr := l(4); addr != 0xffffffff {
				m.poke32(vec, addr)
			}
			return old
		case 6: // Tickcal
			return 20
		}
		return 0
	case 14: // XBIOS
		switch fn {
		case 2, 3: // Physbase, Logbase
			return screenBase
		case 13: // Mfpint(num, vec)
			ch := int(w(2) & 15)
			m.enableMFPChannel(ch, false)
			m.poke32(mfpVector(ch), l(4))
			return 0
		case 14: // Iorec
			return varsBase + vIorec
		case 16: // Keytbl
			return varsBase + vKeytbl
		case 17: // Random
			m.rand = m.rand*3141592621 + 1
			return m.rand >> 8 & 0xffffff
		case 26, 27: // Jdisint / Jenabint(num)
			m.enableMFPChannel(int(w(2)&15), fn == 27)
			return 0
		case 28: // Giaccess(data, reg)
			reg := byte(w(4))
			if reg&0x80 != 0 {
				m.psgWrite(reg&0x0f, byte(w(2)))
				return uint32(byte(w(2)))
			}
			return uint32(m.psgReg[reg&0x0f])
		case 29: // Offgibit(mask)
			m.psgWrite(14, m.psgReg[14]&byte(w(2)))
			return 0
		case 30: // Ongibit(mask)
			m.psgWrite(14, m.psgReg[14]|byte(w(2)))
			return 0
		case 31: // Xbtimer(timer, control, data, vec)
			m.xbtimer(int(w(2)&3), byte(w(4)), byte(w(6)), l(8))
			return 0
		case 34: // Kbdvbase
			return varsBase + vKbdv
		}
		return 0
	}
	return errInvalidFunction
}

// --- memory helpers ---

func (m *machine) poke16(a uint32, v uint16) { binary.BigEndian.PutUint16(m.ram[a:], v) }
func (m *machine) poke32(a, v uint32)        { binary.BigEndian.PutUint32(m.ram[a:], v) }
func (m *machine) putVar32(off, v uint32)    { binary.BigEndian.PutUint32(m.vars[off:], v) }

func (m *machine) peek16(a uint32) uint16 {
	a &= 0xffffff
	if a+2 <= ramSize {
		return binary.BigEndian.Uint16(m.ram[a:])
	}
	v, _ := m.bus.Read(m68kemu.Word, a&^1)
	return uint16(v)
}

func (m *machine) peek32(a uint32) uint32 {
	return uint32(m.peek16(a))<<16 | uint32(m.peek16(a+2))
}

func getBytes(b []byte, off uint32, size m68kemu.Size) uint32 {
	if size == m68kemu.Byte {
		return uint32(b[off])
	}
	return uint32(binary.BigEndian.Uint16(b[off:]))
}

func putBytes(b []byte, off uint32, size m68kemu.Size, v uint32) {
	if size == m68kemu.Byte {
		b[off] = byte(v)
		return
	}
	binary.BigEndian.PutUint16(b[off:], uint16(v))
}

// --- bus devices (the bus splits long accesses into two word accesses) ---

type ramDevice struct{ m *machine }

func (d ramDevice) AddressRange() (uint32, uint32) { return 0, ramSize - 1 }
func (d ramDevice) Read(s m68kemu.Size, a uint32) (uint32, error) {
	return getBytes(d.m.ram, a, s), nil
}
func (d ramDevice) Write(s m68kemu.Size, a, v uint32) error {
	putBytes(d.m.ram, a, s, v)
	return nil
}
func (d ramDevice) Reset() {}

// romDevice is the TOS window: stub code, its variables and the host
// registers.
type romDevice struct{ m *machine }

func (d romDevice) AddressRange() (uint32, uint32) { return romBase, romEnd }
func (d romDevice) Read(s m68kemu.Size, a uint32) (uint32, error) {
	switch {
	case a >= hostBase && a < hostBase+hostSize:
		return getBytes(d.m.host[:], a-hostBase, s), nil
	case a >= varsBase && a < varsBase+varsSize:
		return getBytes(d.m.vars[:], a-varsBase, s), nil
	case a-romBase+uint32(s) <= uint32(len(d.m.tos.code)):
		return getBytes(d.m.tos.code, a-romBase, s), nil
	}
	return 0, nil
}
func (d romDevice) Write(s m68kemu.Size, a, v uint32) error {
	switch {
	case a >= hostBase && a < hostBase+hostSize:
		d.m.hostWrite(a-hostBase, s, v)
	case a >= varsBase && a < varsBase+varsSize:
		putBytes(d.m.vars[:], a-varsBase, s, v)
	}
	return nil
}
func (d romDevice) Reset() {}

// ioDevice covers $FF8000-$FFFFFF: PSG and MFP are emulated, the ACIAs report
// "ready", and every other register just stores what was written.
type ioDevice struct{ m *machine }

func (d ioDevice) AddressRange() (uint32, uint32) { return ioBase, 0xffffff }

func (d ioDevice) Read(s m68kemu.Size, a uint32) (uint32, error) {
	if s == m68kemu.Word {
		return d.readByte(a)<<8 | d.readByte(a+1), nil
	}
	return d.readByte(a), nil
}

func (d ioDevice) readByte(a uint32) uint32 {
	m := d.m
	switch {
	case a >= psgBase && a <= psgEnd:
		return uint32(m.psgReadByte(a))
	case a >= mfpBase && a <= mfpEnd:
		if a&1 == 0 {
			return 0xff
		}
		return uint32(m.mfpRead(a - mfpBase))
	case a == 0xfffc00 || a == 0xfffc04:
		return 0x02 // ACIA status: transmit register empty
	case a == 0xff8209:
		return uint32(byte(m.cpu.Cycles() >> 1)) // video counter: keep moving
	}
	return uint32(m.io[a-ioBase])
}

func (d ioDevice) Write(s m68kemu.Size, a, v uint32) error {
	if s == m68kemu.Word {
		d.writeByte(a, byte(v>>8))
		d.writeByte(a+1, byte(v))
		return nil
	}
	d.writeByte(a, byte(v))
	return nil
}

func (d ioDevice) writeByte(a uint32, v byte) {
	m := d.m
	switch {
	case a >= psgBase && a <= psgEnd:
		m.psgWriteByte(a, v)
	case a >= mfpBase && a <= mfpEnd:
		if a&1 != 0 {
			m.mfpWrite(a-mfpBase, v)
		}
	default:
		m.io[a-ioBase] = v
	}
}

func (d ioDevice) Reset() {}
