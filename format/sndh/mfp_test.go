package sndh

import "testing"

func newTestMFP() *mfp {
	m := &mfp{}
	m.reset()
	return m
}

func TestMFPTimerPeriodAndCounter(t *testing.T) {
	m := newTestMFP()
	m.write(mfpIERA, 1<<5) // Timer A
	m.write(mfpTADR, 10)
	m.write(mfpTACR, 1) // /4: period 40 clocks

	if got := m.read(mfpTADR); got != 10 {
		t.Fatalf("counter right after start = %d, want 10", got)
	}
	m.advance(4 * 3)
	if got := m.read(mfpTADR); got != 7 {
		t.Errorf("counter after 3 ticks = %d, want 7", got)
	}
	m.advance(39)
	if m.ipr != 0 {
		t.Fatal("interrupt before the timeout")
	}
	m.advance(40)
	if m.ipr != 1<<chTimerA {
		t.Fatalf("ipr = %#x, want Timer A pending", m.ipr)
	}
	if got := m.read(mfpTADR); got != 10 {
		t.Errorf("counter after reload = %d, want 10", got)
	}
	if next := m.nextEvent(); next != 80 {
		t.Errorf("next timeout at %d, want 80", next)
	}
}

func TestMFPDataWriteWhileRunning(t *testing.T) {
	m := newTestMFP()
	m.write(mfpTBDR, 4)
	m.write(mfpTBCR, 1) // period 16
	m.advance(8)
	m.write(mfpTBDR, 100) // takes effect at the next reload
	if next := m.nextEvent(); next != 16 {
		t.Fatalf("next timeout %d, want the current period's 16", next)
	}
	m.advance(16)
	if next := m.nextEvent(); next != 16+400 {
		t.Errorf("next timeout %d, want 416 with the new reload value", next)
	}
}

func TestMFPStopFreezesCounter(t *testing.T) {
	m := newTestMFP()
	m.write(mfpTCDR, 50)
	m.write(mfpTCDCR, 0x10) // Timer C /4
	m.advance(4 * 20)
	m.write(mfpTCDCR, 0) // stop
	m.advance(10_000)
	if got := m.read(mfpTCDR); got != 30 {
		t.Errorf("stopped counter = %d, want 30", got)
	}
	if m.nextEvent() != ^uint64(0) {
		t.Error("stopped timer still scheduled")
	}
	m.write(mfpTCDCR, 0x10) // restart from 30
	if next := m.nextEvent(); next != 10_000+4*30 {
		t.Errorf("restart: next timeout %d", next)
	}
}

func TestMFPInterruptPriorityAndEOI(t *testing.T) {
	m := newTestMFP()
	m.write(mfpVR, 0x48) // software end-of-interrupt, vectors at $40
	m.write(mfpIERA, 0xff)
	m.write(mfpIERB, 0xff)
	m.write(mfpIMRA, 0xff)
	m.write(mfpIMRB, 0xff)

	m.raise(chTimerD)
	m.raise(chTimerA)
	ch, ok := m.pending()
	if !ok || ch != chTimerA {
		t.Fatalf("pending = %d,%v, want Timer A first", ch, ok)
	}
	if vec := m.acknowledge(ch); vec != 0x40|chTimerA {
		t.Errorf("vector %#x", vec)
	}
	if _, ok := m.pending(); ok {
		t.Error("Timer D delivered while Timer A is in service")
	}
	m.write(mfpISRA, ^byte(1<<(chTimerA-8))) // end of interrupt
	if ch, ok := m.pending(); !ok || ch != chTimerD {
		t.Errorf("after EOI pending = %d,%v, want Timer D", ch, ok)
	}

	// Masked channels stay pending; disabled ones are dropped.
	m.write(mfpIMRB, 0)
	if _, ok := m.pending(); ok {
		t.Error("masked channel delivered")
	}
	if m.ipr&(1<<chTimerD) == 0 {
		t.Error("masking dropped the pending bit")
	}
	m.write(mfpIERB, 0)
	if m.ipr != 0 {
		t.Error("disabling did not clear the pending bit")
	}
}

func TestMFPAutoEOI(t *testing.T) {
	m := newTestMFP()
	m.write(mfpIERB, 1<<chTimerC)
	m.write(mfpIMRB, 1<<chTimerC)
	m.raise(chTimerC)
	ch, _ := m.pending()
	m.acknowledge(ch)
	if m.isr != 0 {
		t.Error("in-service bit set in automatic end-of-interrupt mode")
	}
}

func TestBestTimerSetting(t *testing.T) {
	for _, hz := range []int{50, 60, 85, 100, 200, 1000} {
		ctrl, data := bestTimerSetting(hz)
		got := float64(mfpClock) / float64(mfpPrescale[ctrl]*reloadValue(data))
		if d := got - float64(hz); d > float64(hz)*0.005 || d < -float64(hz)*0.005 {
			t.Errorf("%d Hz: ctrl %d data %d gives %.2f Hz", hz, ctrl, data, got)
		}
	}
	// 200 Hz is exact: TOS uses /64 and 192.
	if ctrl, data := bestTimerSetting(200); mfpPrescale[ctrl]*reloadValue(data) != 64*192 {
		t.Errorf("200 Hz: ctrl %d data %d", ctrl, data)
	}
}
