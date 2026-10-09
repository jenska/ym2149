package ym

import (
	"math"
	"testing"
	"time"
)

func drainAll(t *testing.T, p *Player, n int) []float32 {
	t.Helper()
	out := make([]float32, 0, n)
	buf := make([]float32, 1024)
	for len(out) < n {
		req := buf
		if rem := n - len(out); rem < len(req) {
			req = req[:rem]
		}
		got := p.DrainMonoF32(req)
		out = append(out, req[:got]...)
		if got == 0 {
			break
		}
	}
	return out
}

func rms(s []float32) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

// steadyToneYM3 is a one-note square wave on channel A held for n frames.
func steadyToneYM3(n int) *Song {
	frames := make([][14]byte, n)
	for i := range frames {
		frames[i] = [14]byte{
			0x40, 0x00, // A period = 64
			0, 0, 0, 0,
			0,    // noise
			0x3e, // mixer: tone A only
			0x0c, // A volume 12
			0, 0,
			0, 0, 0,
		}
	}
	s, err := Parse(buildYM3(frames, nil))
	if err != nil {
		panic(err)
	}
	return s
}

func TestPlayerProducesTone(t *testing.T) {
	p, err := NewPlayer(steadyToneYM3(25), PlayerConfig{SampleRate: 48_000})
	if err != nil {
		t.Fatal(err)
	}
	if p.OutputSampleRate() != 48_000 {
		t.Fatalf("rate = %d", p.OutputSampleRate())
	}

	// 25 frames @ 50 Hz == 0.5 s == 24000 samples.
	got := drainAll(t, p, 24_000)
	if len(got) < 23_000 {
		t.Fatalf("only %d samples produced", len(got))
	}
	if r := rms(got); r < 0.02 {
		t.Fatalf("output too quiet: rms=%.4f", r)
	}
}

func TestPlayerDeterministic(t *testing.T) {
	a := drainAll(t, mustPlayer(t, steadyToneYM3(10)), 9600)
	b := drainAll(t, mustPlayer(t, steadyToneYM3(10)), 9600)
	if len(a) != len(b) {
		t.Fatalf("length mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestPlayerFinishAndLoop(t *testing.T) {
	// Non-looping: drain past the end, expect a short read and Finished().
	p := mustPlayer(t, steadyToneYM3(4))
	_ = drainAll(t, p, 100_000)
	if !p.Finished() {
		t.Fatal("player should be finished")
	}
	buf := make([]float32, 128)
	if n := p.DrainMonoF32(buf); n != 0 {
		t.Fatalf("post-finish drain returned %d", n)
	}

	// Looping: never finishes, keeps producing sound.
	lp, err := NewPlayer(steadyToneYM3(4), PlayerConfig{SampleRate: 48_000, Loop: true})
	if err != nil {
		t.Fatal(err)
	}
	got := drainAll(t, lp, 48_000)
	if len(got) != 48_000 || lp.Finished() {
		t.Fatalf("loop: %d samples, finished=%v", len(got), lp.Finished())
	}
	if rms(got) < 0.02 {
		t.Fatal("looping output went silent")
	}
}

func TestPlayerYM6Effects(t *testing.T) {
	// A YM6 song that exercises every timer effect across its frames, plus a
	// digidrum. The point is to prove the scheduler stays sane (no panic, no
	// runaway sample counts, audible output), not bit-exact reproduction.
	const n = 30
	frames := make([][16]byte, n)
	drum := make([]byte, 64)
	for i := range drum {
		drum[i] = byte(i * 4)
	}
	for i := range frames {
		f := &frames[i]
		f[0], f[1] = 0x80, 0x00 // channel A tone
		f[7] = 0x38             // tone A
		f[8] = 0x0a
		f[11] = 0x10
		f[13] = 0x0a
		switch i % 3 {
		case 0: // SID voice A: type 00, voice 1 -> code nibble 0x10
			f[1] = 0x10
			f[6] = 4 << 5 // prediv index 4
			f[14] = 20    // count
		case 1: // digidrum voice B: type 01 (0x40), voice 2 -> 0x40|0x20 = 0x60
			f[3] = 0x60
			f[8] = 0x00    // drum #0
			f[8] |= 5 << 5 // prediv index 5
			f[15] = 8
		case 2: // sync-buzzer voice C: type 11 (0xc0), voice 3 -> 0xc0|0x30 = 0xf0
			f[1] = 0xf0
			f[6] = 3 << 5
			f[14] = 40
			f[10] = 0x0c // buzzer shape source (r8+voice low nibble)
		}
	}
	raw := buildYM56("YM6!", frames, [][]byte{drum}, false, false, 50, atariSTClock, 0)

	p, err := NewPlayerFromBytes(raw, PlayerConfig{SampleRate: 44_100})
	if err != nil {
		t.Fatal(err)
	}
	got := drainAll(t, p, 44_100*n/50)
	want := 44_100 * n / 50
	if len(got) < want-1000 || len(got) > want+1000 {
		t.Fatalf("sample count %d, want ~%d", len(got), want)
	}
	if rms(got) < 0.01 {
		t.Fatalf("effect song too quiet: rms=%.4f", rms(got))
	}
}

func TestPlayerChannelTaps(t *testing.T) {
	// Tone only on channel A (mixer 0x3e). Channel A tap should carry it;
	// B and C taps stay silent. Mono drain is inert in taps mode.
	p, err := NewPlayer(steadyToneYM3(20), PlayerConfig{SampleRate: 48_000, ChannelTaps: true})
	if err != nil {
		t.Fatal(err)
	}
	if !p.ChannelTapsEnabled() {
		t.Fatal("taps not enabled")
	}
	if n := p.DrainMonoF32(make([]float32, 64)); n != 0 {
		t.Fatalf("DrainMonoF32 returned %d in taps mode", n)
	}

	// Drain the three channels in lockstep, the way renderer/stereo does.
	var got [3][]float32
	var buf [3][]float32
	for ch := range 3 {
		buf[ch] = make([]float32, 1024)
	}
	for {
		n := p.DrainChannelF32(0, buf[0])
		p.DrainChannelF32(1, buf[1])
		p.DrainChannelF32(2, buf[2])
		if n == 0 {
			break
		}
		for ch := range 3 {
			got[ch] = append(got[ch], buf[ch][:n]...)
		}
	}

	if rms(got[0]) < 0.02 {
		t.Fatalf("channel A tap silent: rms=%.4f", rms(got[0]))
	}
	if rms(got[1]) > 1e-4 || rms(got[2]) > 1e-4 {
		t.Fatalf("channels B/C should be silent: B=%.6f C=%.6f", rms(got[1]), rms(got[2]))
	}
}

func mustPlayer(t *testing.T, s *Song) *Player {
	t.Helper()
	p, err := NewPlayer(s, PlayerConfig{SampleRate: 48_000})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlayerSeek(t *testing.T) {
	// Frame i plays tone period i+1, so the period register shows which
	// frame is playing; frame 2 sets envelope shape 0x0a.
	frames := make([][14]byte, 50)
	for i := range frames {
		frames[i] = [14]byte{byte(i + 1), 0, 0, 0, 0, 0, 0, 0x3e, 0x0c, 0, 0, 0, 0, 0xff}
	}
	frames[2][13] = 0x0a
	loop := 10
	s, err := Parse(buildYM3(frames, &loop))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 3 {
		t.Fatalf("version %d", s.Version)
	}
	period := func(p *Player) byte {
		p.Chip().SelectRegister(0)
		return p.Chip().ReadData()
	}

	p, err := NewPlayer(s, PlayerConfig{SampleRate: 8000, Loop: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Duration() != time.Second {
		t.Fatalf("Duration = %v, want 1s", p.Duration())
	}
	drainAll(t, p, 800) // 5 frames

	p.Seek(600 * time.Millisecond) // frame 30
	if p.Frame() != 30 || p.Position() != 600*time.Millisecond {
		t.Fatalf("after seek: frame %d position %v", p.Frame(), p.Position())
	}
	drainAll(t, p, 160) // play frame 30
	if got := period(p); got != 31 {
		t.Errorf("playing period %d after seeking to frame 30, want 31", got)
	}
	p.Chip().SelectRegister(13)
	if got := p.Chip().ReadData(); got != 0x0a {
		t.Errorf("envelope shape %#x, want 0x0a restored", got)
	}

	// Past the end: lands inside the loop, position keeps counting.
	p.Seek(1500 * time.Millisecond) // 75 frames = 50 + 25 -> loop frame 10 + 25
	if p.Frame() != 35 || p.Position() != 1500*time.Millisecond {
		t.Errorf("looped seek: frame %d position %v, want 35 and 1.5s", p.Frame(), p.Position())
	}

	// Without Loop a seek past the end finishes the song.
	np := mustPlayer(t, s)
	np.Seek(2 * time.Second)
	if !np.Finished() || np.DrainMonoF32(make([]float32, 16)) != 0 {
		t.Error("seek past the end of a non-looping song should finish it")
	}
	np.Seek(0)
	if np.Finished() || len(drainAll(t, np, 1000)) != 1000 {
		t.Error("seek back to the start should resume playback")
	}
}
