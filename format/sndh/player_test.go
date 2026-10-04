package sndh

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jenska/m68kasm"
	"github.com/jenska/m68kdasm"
)

// testTune assembles an SNDH image at tuneBase: the three entry branches,
// the given header tags (between 'SNDH' and 'HDNS') and code, which must
// define init, exit and play.
type testTune struct {
	data   []byte
	labels map[string]uint32
}

func buildTune(t *testing.T, tags, code string) testTune {
	t.Helper()
	hdr := []byte("SNDH" + tags + "HDNS")
	var sb strings.Builder
	fmt.Fprintf(&sb, "        .org    $%x\n        bra.w   init\n        bra.w   exit\n        bra.w   play\n", tuneBase)
	for i := 0; i < len(hdr); i += 16 {
		vals := make([]string, 0, 16)
		for _, b := range hdr[i:min(i+16, len(hdr))] {
			vals = append(vals, fmt.Sprintf("$%02x", b))
		}
		sb.WriteString("        dc.b    " + strings.Join(vals, ",") + "\n")
	}
	sb.WriteString("        .even\n")
	sb.WriteString(code)
	res, err := m68kasm.AssembleStringDetailed(sb.String())
	if err != nil {
		t.Fatalf("assembling test tune: %v", err)
	}
	return testTune{data: res.Bytes, labels: res.Labels}
}

func (tt testTune) player(t *testing.T, cfg PlayerConfig) *Player {
	t.Helper()
	p, err := NewPlayerFromBytes(tt.data, cfg)
	if err != nil {
		t.Fatalf("NewPlayerFromBytes: %v", err)
	}
	return p
}

func (tt testTune) long(t *testing.T, p *Player, label string) uint32 {
	t.Helper()
	a, ok := tt.labels[label]
	if !ok {
		t.Fatalf("no label %q", label)
	}
	return binary.BigEndian.Uint32(p.m.ram[a:])
}

// render drains d of mono (or channel A) audio and returns it.
func render(p *Player, d time.Duration) []float32 {
	buf := make([]float32, int(d.Seconds()*float64(p.OutputSampleRate())))
	var n int
	if p.ChannelTapsEnabled() {
		n = p.DrainChannelF32(0, buf)
	} else {
		n = p.DrainMonoF32(buf)
	}
	return buf[:n]
}

func peakToPeak(s []float32) float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range s {
		lo, hi = min(lo, float64(v)), max(hi, float64(v))
	}
	return hi - lo
}

// countingCode counts PLAY calls in 'count'.
const countingCode = `
init:   rts
exit:   rts
play:   lea     count,a0
        addq.l  #1,(a0)
        rts
        .even
count:  dc.l    0
`

func TestReplayRates(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want float64
	}{
		{"TC50\x00", 50},
		{"TC200\x00", 200},
		{"TC100\x00", 100},
		{"TC85\x00", 85},
		{"TA100\x00", 100},
		{"TB60\x00", 60},
		{"TD150\x00", 150},
		{"!V50\x00", float64(cpuClock) / vblCycles},
		{"", 50}, // Timer C at 50 Hz by default
	} {
		t.Run(strings.TrimRight(tc.tag, "\x00"), func(t *testing.T) {
			tt := buildTune(t, tc.tag, countingCode)
			p := tt.player(t, PlayerConfig{SampleRate: 8000})
			render(p, 4*time.Second)
			if p.Err() != nil {
				t.Fatal(p.Err())
			}
			got := float64(tt.long(t, p, "count")) / p.Position().Seconds()
			if math.Abs(got-tc.want) > tc.want*0.01 {
				t.Errorf("PLAY rate %.2f Hz, want %.2f", got, tc.want)
			}
		})
	}
}

func TestDriverTimerInterrupt(t *testing.T) {
	// INIT installs a Timer A handler through Xbtimer (/200, data 123 =
	// 99.9 Hz) that acknowledges itself in software end-of-interrupt mode.
	tt := buildTune(t, "TC50\x00", `
init:   lea     timera,a0
        move.l  a0,-(sp)
        move.w  #123,-(sp)
        move.w  #7,-(sp)
        move.w  #0,-(sp)
        move.w  #31,-(sp)
        trap    #14
        lea     (12,sp),sp
        rts
exit:   rts
play:   rts
timera: move.l  a0,-(sp)
        lea     ticks,a0
        addq.l  #1,(a0)
        move.l  (sp)+,a0
        move.b  #$df,$fffa0f
        rte
        .even
ticks:  dc.l    0
`)
	p := tt.player(t, PlayerConfig{SampleRate: 8000})
	render(p, 3*time.Second)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	want := float64(mfpClock) / (200 * 123)
	got := float64(tt.long(t, p, "ticks")) / p.Position().Seconds()
	if math.Abs(got-want) > 1 {
		t.Errorf("Timer A rate %.2f Hz, want %.2f", got, want)
	}
}

func TestPSGOutputAndGiaccess(t *testing.T) {
	// INIT programs a tone on channel A, half via the hardware registers and
	// the volume via XBIOS Giaccess, then reads the volume back.
	tt := buildTune(t, "TC50\x00", `
init:   move.b  #0,$ff8800
        move.b  #$40,$ff8802
        move.b  #1,$ff8800
        move.b  #0,$ff8802
        move.l  #$07003e00,$ff8800
        move.w  #$88,-(sp)
        move.w  #15,-(sp)
        move.w  #28,-(sp)
        trap    #14
        addq.l  #6,sp
        move.w  #8,-(sp)
        move.w  #0,-(sp)
        move.w  #28,-(sp)
        trap    #14
        addq.l  #6,sp
        lea     vol,a0
        move.l  d0,(a0)
        move.b  #7,$ff8800
        moveq   #0,d0
        move.b  $ff8800,d0
        lea     mixer,a0
        move.l  d0,(a0)
        rts
exit:   rts
play:   rts
        .even
vol:    dc.l    0
mixer:  dc.l    0
`)
	p := tt.player(t, PlayerConfig{SampleRate: 48000})
	pcm := render(p, 500*time.Millisecond)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	if v := tt.long(t, p, "vol"); v != 15 {
		t.Errorf("Giaccess read back volume %d, want 15", v)
	}
	if v := tt.long(t, p, "mixer"); v != 0x3e {
		t.Errorf("mixer read back %#x, want 0x3e", v)
	}
	if peakToPeak(pcm[len(pcm)/2:]) < 0.1 {
		t.Error("tone channel produced no audible output")
	}
	if got := p.Chip().Cycles(); got == 0 {
		t.Error("PSG never stepped")
	}
}

func TestChannelTaps(t *testing.T) {
	tt := buildTune(t, "TC50\x00", `
init:   move.l  #$00004000,$ff8800
        move.l  #$07003e00,$ff8800
        move.l  #$08000f00,$ff8800
        rts
exit:   rts
play:   rts
`)
	p := tt.player(t, PlayerConfig{SampleRate: 48000, ChannelTaps: true})
	if p.DrainMonoF32(make([]float32, 16)) != 0 {
		t.Error("DrainMonoF32 should be empty with channel taps")
	}
	if pcm := render(p, 200*time.Millisecond); peakToPeak(pcm) < 0.1 {
		t.Error("channel A tap is silent")
	}
	silent := make([]float32, 4800)
	n := p.DrainChannelF32(1, silent)
	if peakToPeak(silent[:n]) > 0.01 {
		t.Error("channel B tap should be silent")
	}
}

func TestTOSCalls(t *testing.T) {
	tt := buildTune(t, "TC50\x00", `
init:   move.l  #1,-(sp)            ; Super(1): supervisor mode?
        move.w  #$20,-(sp)
        trap    #1
        addq.l  #6,sp
        lea     super,a0
        move.l  d0,(a0)
        move.l  #1000,-(sp)         ; Malloc(1000)
        move.w  #$48,-(sp)
        trap    #1
        addq.l  #6,sp
        lea     block,a0
        move.l  d0,(a0)
        lea     func,a0             ; Supexec(func)
        move.l  a0,-(sp)
        move.w  #38,-(sp)
        trap    #14
        addq.l  #6,sp
        lea     supex,a0
        move.l  d0,(a0)
        move.l  #-1,-(sp)           ; Setexc($45, -1): read Timer C vector
        move.w  #$45,-(sp)
        move.w  #5,-(sp)
        trap    #13
        addq.l  #8,sp
        lea     vector,a0
        move.l  d0,(a0)
        move.w  #$4f,-(sp)          ; unknown GEMDOS call
        trap    #1
        addq.l  #2,sp
        lea     unknown,a0
        move.l  d0,(a0)
        rts
func:   moveq   #42,d0
        rts
exit:   rts
play:   rts
        .even
super:  dc.l    0
block:  dc.l    0
supex:  dc.l    0
vector: dc.l    0
unknown: dc.l   0
`)
	p := tt.player(t, PlayerConfig{SampleRate: 8000})
	render(p, 100*time.Millisecond)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	img, _ := loadTOS()
	for _, c := range []struct {
		label string
		want  uint32
	}{
		{"super", 0xffffffff},
		{"block", heapBase},
		{"supex", 42},
		{"vector", img.label("timer_c")},
		{"unknown", errInvalidFunction},
	} {
		if got := tt.long(t, p, c.label); got != c.want {
			t.Errorf("%s = %#x, want %#x", c.label, got, c.want)
		}
	}
}

func TestSubtuneSelection(t *testing.T) {
	tt := buildTune(t, "##04\x00#!02", `
init:   lea     sub,a0
        move.l  d0,(a0)
        rts
exit:   rts
play:   rts
        .even
sub:    dc.l    0
`)
	for cfgSub, want := range map[int]uint32{0: 2, 3: 3, 4: 4} {
		p := tt.player(t, PlayerConfig{SampleRate: 8000, Subtune: cfgSub})
		render(p, 20*time.Millisecond)
		if got := tt.long(t, p, "sub"); got != want || p.Subtune() != int(want) {
			t.Errorf("Subtune %d: INIT saw d0=%d, Subtune()=%d, want %d", cfgSub, got, p.Subtune(), want)
		}
	}
	if _, err := NewPlayerFromBytes(tt.data, PlayerConfig{Subtune: 5}); err == nil {
		t.Error("subtune 5 of 4 accepted")
	}
}

func TestDurationAndLoop(t *testing.T) {
	frms := string([]byte{0, 0, 0, 50}) // 50 frames at 50 Hz: 1 s
	tt := buildTune(t, "TC50\x00FRMS"+frms, countingCode)

	p := tt.player(t, PlayerConfig{SampleRate: 8000})
	if p.Duration() != time.Second {
		t.Fatalf("Duration = %v, want 1s", p.Duration())
	}
	pcm := render(p, 3*time.Second)
	if !p.Finished() || p.Err() != nil {
		t.Fatalf("Finished=%v Err=%v after the subtune's length", p.Finished(), p.Err())
	}
	if secs := float64(len(pcm)) / 8000; secs < 0.95 || secs > 1.2 {
		t.Errorf("rendered %.2fs, want about 1s", secs)
	}

	p = tt.player(t, PlayerConfig{SampleRate: 8000, Loop: true})
	if pcm := render(p, 3*time.Second); len(pcm) != 3*8000 || p.Finished() {
		t.Errorf("looping player stopped after %d samples", len(pcm))
	}
}

func TestDriverCrash(t *testing.T) {
	tt := buildTune(t, "", `
init:   illegal
exit:   rts
play:   rts
`)
	p := tt.player(t, PlayerConfig{SampleRate: 8000})
	pcm := render(p, time.Second)
	if p.Err() == nil || !p.Finished() {
		t.Fatalf("Err=%v Finished=%v after an illegal instruction", p.Err(), p.Finished())
	}
	if !strings.Contains(p.Err().Error(), "vector 4") {
		t.Errorf("error %q does not name the illegal-instruction vector", p.Err())
	}
	if len(pcm) >= 8000 {
		t.Error("crashed player kept producing audio")
	}
}

func TestICEPackedTune(t *testing.T) {
	tt := buildTune(t, "TITLPacked\x00TC50\x00", countingCode)
	p, err := NewPlayerFromBytes(icePack(tt.data), PlayerConfig{SampleRate: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if !p.File().Packed || p.File().Title != "Packed" {
		t.Fatalf("Packed=%v Title=%q", p.File().Packed, p.File().Title)
	}
	render(p, time.Second)
	if n := tt.long(t, p, "count"); n < 45 {
		t.Errorf("only %d PLAY calls in a second", n)
	}
}

func TestDeterministic(t *testing.T) {
	tt := buildTune(t, "TC50\x00", `
init:   move.l  #$07003e00,$ff8800
        move.l  #$08000f00,$ff8800
        rts
exit:   rts
play:   lea     period,a0
        addq.w  #7,(a0)
        move.b  #0,$ff8800
        move.b  (1,a0),$ff8802
        rts
        .even
period: dc.w    0
`)
	a := render(tt.player(t, PlayerConfig{SampleRate: 22050}), time.Second)
	b := render(tt.player(t, PlayerConfig{SampleRate: 22050}), time.Second)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs", i)
		}
	}
}

// TestTOSStubLabels guards against assembler layout bugs (m68kasm v1.6.1
// mis-sizes some forms when laying out labels): every exported label must
// start an instruction in the assembled stub.
func TestTOSStubLabels(t *testing.T) {
	img, err := loadTOS()
	if err != nil {
		t.Fatal(err)
	}
	const headerSize = 0x30 // OS header data before os_start
	starts := map[uint32]bool{}
	for pc := uint32(romBase + headerSize); pc < romBase+uint32(len(img.code)); {
		ins, err := m68kdasm.Decode(img.code[pc-romBase:], pc)
		if err != nil {
			t.Fatalf("decode at %06x: %v", pc, err)
		}
		starts[pc] = true
		pc += ins.Size
	}
	for name, addr := range img.addr {
		if addr < romBase+headerSize || strings.HasPrefix(name, "os_entry") {
			continue
		}
		if !starts[addr] {
			t.Errorf("label %s at %06x is not an instruction boundary", name, addr)
		}
	}
}

func TestSeek(t *testing.T) {
	// PLAY counts its calls and plays the count as a tone period, so after a
	// seek both the call count and the PSG registers must match the target.
	tt := buildTune(t, "TC50\x00", `
init:   move.l  #$07003e00,$ff8800
        move.l  #$08000f00,$ff8800
        rts
exit:   rts
play:   lea     count,a0
        addq.l  #1,(a0)
        move.b  #0,$ff8800
        move.b  (3,a0),$ff8802
        rts
        .even
count:  dc.l    0
`)
	p := tt.player(t, PlayerConfig{SampleRate: 8000})
	render(p, 500*time.Millisecond)

	if err := p.Seek(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if pos := p.Position(); pos < 10*time.Second || pos > 10*time.Second+10*time.Millisecond {
		t.Errorf("Position after seek = %v", pos)
	}
	n := tt.long(t, p, "count")
	if n < 499 || n > 501 {
		t.Errorf("%d PLAY calls after seeking to 10s, want 500", n)
	}
	render(p, 100*time.Millisecond)
	p.Chip().SelectRegister(0)
	if got, want := p.Chip().ReadData(), byte(tt.long(t, p, "count")); got != want {
		t.Errorf("PSG period register %d, want %d", got, want)
	}

	// Backwards: restart and skip again.
	if err := p.Seek(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if n := tt.long(t, p, "count"); n < 99 || n > 101 {
		t.Errorf("%d PLAY calls after seeking back to 2s, want 100", n)
	}
	if pcm := render(p, 200*time.Millisecond); peakToPeak(pcm) < 0.1 {
		t.Error("silent after seeking")
	}
}
