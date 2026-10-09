package main

import (
	"archive/zip"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tinySNDH is a valid two-subtune SNDH image whose INIT programs a tone on
// voice A and whose EXIT/PLAY do nothing.
func tinySNDH(title string) []byte {
	hdr := []byte("SNDHTITL" + title + "\x00##02TC50\x00FRMS\x00\x00\x00\x32\x00\x00\x00\x00HDNS")
	if len(hdr)%2 != 0 {
		hdr = append(hdr, 0)
	}
	const initBody = 3 * 10 // three move.l #imm,abs.l
	code := 12 + len(hdr)
	var img []byte
	for off := 0; off < 12; off += 4 {
		target := code + initBody // EXIT and PLAY: the final rts
		if off == 0 {
			target = code
		}
		img = binary.BigEndian.AppendUint16(img, 0x6000) // bra.w
		img = binary.BigEndian.AppendUint16(img, uint16(target-off-2))
	}
	img = append(img, hdr...)
	for _, l := range []uint32{0x00004000, 0x07003e00, 0x08000f00} {
		img = binary.BigEndian.AppendUint16(img, 0x23fc) // move.l #imm,abs.l
		img = binary.BigEndian.AppendUint32(img, l)
		img = binary.BigEndian.AppendUint32(img, 0xff8800)
	}
	return binary.BigEndian.AppendUint16(img, 0x4e75) // rts
}

func writeTunes(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"b.sndh", "a.snd"} {
		if err := os.WriteFile(filepath.Join(dir, n), tinySNDH(strings.ToUpper(n[:1])+"-tune"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignored"), 0o644)

	zf, _ := os.Create(filepath.Join(dir, "pack.zip"))
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("music/c.sndh")
	w.Write(tinySNDH("C-tune"))
	w, _ = zw.Create("music/notes.txt")
	w.Write([]byte("ignored"))
	zw.Close()
	zf.Close()
	return dir
}

func TestBuildPlaylist(t *testing.T) {
	dir := writeTunes(t)
	list, err := buildPlaylist([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.name)
	}
	if got := strings.Join(names, ","); got != "a.snd,b.sndh,music/c.sndh" {
		t.Fatalf("playlist %s", got)
	}
	f, err := list[2].parse()
	if err != nil || f.Title != "C-tune" {
		t.Fatalf("zip entry: %v %v", f, err)
	}
	if _, err := buildPlaylist([]string{filepath.Join(dir, "readme.txt")}); err != nil {
		t.Fatal("an explicitly named file is taken as is")
	}
	if _, err := buildPlaylist([]string{t.TempDir()}); err == nil {
		t.Error("empty directory accepted")
	}
}

func newTestApp(t *testing.T) *app {
	t.Helper()
	list, err := buildPlaylist([]string{writeTunes(t)})
	if err != nil {
		t.Fatal(err)
	}
	return newApp(newEngine(8000, modeContinuous, panABC, time.Minute), list, 8000)
}

func frameText(c *canvas) string {
	var sb strings.Builder
	for y := range c.h {
		for x := range c.w {
			sb.WriteRune(c.cells[y*c.w+x].r)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestAppPlaysAndDraws(t *testing.T) {
	a := newTestApp(t)
	if err := a.play(0, 0); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8000*8/2)
	a.eng.Read(buf)

	frame := frameText(a.draw(100, 30))
	for _, want := range []string{"A-tune", "Subtune 1 of 2", "Timer C at 50 Hz", "Voice A", "♪ a.snd", "Subtunes 1/2", "00:01"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
	if !strings.ContainsAny(frame, "⠁⠂⠄⡀⠈⠐⠠⢀⣀⠤⠒⠉") {
		t.Error("no oscilloscope trace drawn")
	}
	if !strings.Contains(frameText(a.draw(40, 10)), "too small") {
		t.Error("tiny terminal not reported")
	}
	out := a.draw(100, 30).render(false)
	if strings.Contains(out, "\x1b[0;36m") || !strings.HasPrefix(out, "\x1b[H") {
		t.Error("colourless render still emits colours")
	}
}

func TestAppKeys(t *testing.T) {
	a := newTestApp(t)
	a.play(0, 0)

	a.handle(event{key: keyRune, r: 'n'})
	if a.playFile != 0 || a.playSub != 2 {
		t.Fatalf("n: playing %d/%d, want file 0 subtune 2", a.playFile, a.playSub)
	}
	a.handle(event{key: keyRune, r: 'n'})
	if a.playFile != 1 || a.playSub != 1 {
		t.Fatalf("n across files: playing %d/%d", a.playFile, a.playSub)
	}
	a.handle(event{key: keyRune, r: 'p'})
	if a.playFile != 0 || a.playSub != 2 {
		t.Fatalf("p: playing %d/%d", a.playFile, a.playSub)
	}
	a.handle(event{key: keyRune, r: 'P'}) // previous file wraps around
	if a.playFile != 2 {
		t.Fatalf("P: playing file %d, want 2", a.playFile)
	}

	a.handle(event{key: keyHome})
	a.handle(event{key: keyDown})
	a.handle(event{key: keyEnter})
	if a.playFile != 1 || a.playSub != 1 {
		t.Fatalf("select+enter: playing %d/%d", a.playFile, a.playSub)
	}
	a.handle(event{key: keyTab})
	a.handle(event{key: keyDown})
	a.handle(event{key: keyEnter})
	if a.playSub != 2 {
		t.Fatalf("subtune list enter: playing subtune %d", a.playSub)
	}

	a.handle(event{key: keyRune, r: 'm'})
	if a.eng.snapshot(0).mode != modeRandom {
		t.Error("m did not cycle the play mode")
	}
	a.handle(event{key: keyRune, r: 's'})
	if a.eng.snapshot(0).pan != panACB {
		t.Error("s did not cycle the stereo mode")
	}
	a.handle(event{key: keyRune, r: ' '})
	if !a.eng.snapshot(0).paused {
		t.Error("space did not pause")
	}
	a.handle(event{key: keyRune, r: ' '})
	a.handle(event{key: keyRight})
	if pos := a.eng.snapshot(0).position; pos < 5*time.Second {
		t.Errorf("→ seeked to %v", pos)
	}
	if a.handle(event{key: keyRune, r: 'q'}) {
		t.Error("q did not quit")
	}
}

func TestTuneEndAdvancesInContinuousMode(t *testing.T) {
	a := newTestApp(t)
	a.play(0, 0)
	// Each subtune is 50 frames = 1 s long.
	buf := make([]byte, 8000*8/10)
	for range 15 {
		a.eng.Read(buf)
	}
	select {
	case <-a.eng.endCh:
	default:
		t.Fatal("no end signal after the subtune's length")
	}
	a.tuneEnded()
	if a.playSub != 2 {
		t.Errorf("continuous mode moved to subtune %d, want 2", a.playSub)
	}

	a.eng.setMode(modeSingle)
	a.play(0, 1)
	for range 15 {
		a.eng.Read(buf)
	}
	<-a.eng.endCh
	a.tuneEnded()
	if a.playSub != 1 || !a.eng.snapshot(0).ended {
		t.Error("single mode should stop at the end")
	}
}

func TestClickSeeksAndSelects(t *testing.T) {
	a := newTestApp(t)
	a.play(0, 1)
	a.draw(100, 30)
	a.click(a.lay.barX+a.lay.barW/2, a.lay.barY)
	if pos := a.eng.snapshot(0).position; pos < 400*time.Millisecond || pos > 600*time.Millisecond {
		t.Errorf("clicking the middle of the bar seeked to %v, want ~0.5s", pos)
	}
	a.click(a.lay.files.x+2, a.lay.files.y+2)
	if a.fileSel != 2 || a.focus != focusFiles {
		t.Errorf("click selected file %d", a.fileSel)
	}
	a.click(a.lay.files.x+2, a.lay.files.y+2)
	if a.playFile != 2 {
		t.Error("second click did not play")
	}
}

func TestParseInput(t *testing.T) {
	evs := parseInput([]byte("q\x1b[A\x1b[B\x1b[C\x1b[D\x1b[5~\x1b[6~\r\t\x1b[<0;10;5M\x1b[<64;1;1M\x03é"))
	want := []event{
		{key: keyRune, r: 'q'}, {key: keyUp}, {key: keyDown}, {key: keyRight}, {key: keyLeft},
		{key: keyPgUp}, {key: keyPgDn}, {key: keyEnter}, {key: keyTab},
		{key: keyClick, x: 9, y: 4}, {key: keyWheelUp}, {key: keyQuit}, {key: keyRune, r: 'é'},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d events %v", len(evs), evs)
	}
	for i := range want {
		if evs[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, evs[i], want[i])
		}
	}
	if evs := parseInput([]byte("\x1b[<0;3;3m")); len(evs) != 0 {
		t.Error("mouse release should be ignored")
	}
}

func TestHelpers(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "00:00", 75 * time.Second: "01:15", 3725 * time.Second: "1:02:05", -time.Second: "00:00"} {
		if got := fmtDur(d); got != want {
			t.Errorf("fmtDur(%v) = %s, want %s", d, got, want)
		}
	}
	if scrollTop(0, 12, 10) != 3 || scrollTop(5, 2, 10) != 2 || scrollTop(3, 5, 10) != 3 {
		t.Error("scrollTop")
	}
	if clean("caf\xe9\x01") != "caf??" {
		t.Errorf("clean = %q", clean("caf\xe9\x01"))
	}
	for _, s := range []string{"single", "LOOP", "Continuous", "random"} {
		if _, err := parseMode(s); err != nil {
			t.Error(err)
		}
	}
	if _, err := parsePan("acb"); err != nil {
		t.Error(err)
	}
}

func TestExportWAV(t *testing.T) {
	f, err := (&entry{load: func() ([]byte, error) { return tinySNDH("W"), nil }}).parse()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "out.wav")
	var last float64
	if err := exportWAV(path, f, 1, 500*time.Millisecond, 8000, panABC.panning(), func(p float64) { last = p }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data[:4]) != "RIFF" || string(data[8:16]) != "WAVEfmt " {
		t.Fatal("not a WAV file")
	}
	if got, want := len(data)-44, 4000*4; got != want {
		t.Errorf("%d data bytes, want %d", got, want)
	}
	if binary.LittleEndian.Uint32(data[40:]) != uint32(len(data)-44) {
		t.Error("data chunk size mismatch")
	}
	if last != 1 {
		t.Errorf("final progress %v", last)
	}
}

// tinyYM is a YM3! dump of n frames (50 Hz) holding a tone on voice A.
func tinyYM(n int) []byte {
	out := []byte("YM3!")
	plane := make([]byte, 14*n)
	for f := range n {
		for r, v := range []byte{0x40, 0, 0, 0, 0, 0, 0, 0x3e, 0x0f, 0, 0, 0, 0, 0} {
			plane[r*n+f] = v
		}
	}
	return append(out, plane...)
}

func newMixedApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1-first.sndh"), tinySNDH("S-tune"), 0o644)
	os.WriteFile(filepath.Join(dir, "2-second.ym"), tinyYM(50), 0o644)
	list, err := buildPlaylist([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("playlist has %d entries, want 2", len(list))
	}
	return newApp(newEngine(8000, modeContinuous, panABC, time.Minute), list, 8000)
}

func TestYMPlayback(t *testing.T) {
	a := newMixedApp(t)
	if err := a.play(1, 0); err != nil {
		t.Fatal(err)
	}
	f, _ := a.playingFile()
	if f.Format != "YM3" || f.Subtunes != 1 || f.Duration(1) != time.Second {
		t.Fatalf("YM tune: format %q, %d subtunes, %v", f.Format, f.Subtunes, f.Duration(1))
	}
	a.eng.Read(make([]byte, 8000*8/4))
	frame := frameText(a.draw(100, 30))
	for _, want := range []string{"Format", "YM3 register dump, 50 frames", "Rate", "50 Hz, PSG at 2.000 MHz", "Subtune 1 of 1", "♪ 2-second.ym", "♪  1 *", "00:01"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q:\n%s", want, frame)
		}
	}
	if !strings.ContainsAny(frame, "⠁⠂⠄⡀⠈⠐⠠⢀⣀⠤⠒⠉") {
		t.Error("no oscilloscope trace for the YM tune")
	}

	a.seekBy(500 * time.Millisecond)
	if pos := a.eng.snapshot(0).position; pos < 700*time.Millisecond || pos > 800*time.Millisecond {
		t.Errorf("YM seek landed at %v, want ~0.75s", pos)
	}

	// The 1 s YM tune ends; continuous mode wraps to the SNDH file.
	buf := make([]byte, 8000*8/10)
	for range 5 {
		a.eng.Read(buf)
	}
	select {
	case <-a.eng.endCh:
	default:
		t.Fatal("no end signal for the YM tune")
	}
	a.tuneEnded()
	if a.playFile != 0 || a.playSub != 1 {
		t.Errorf("after the YM tune: playing %d/%d, want the SNDH file", a.playFile, a.playSub)
	}
	a.handle(event{key: keyRune, r: 'n'})
	a.handle(event{key: keyRune, r: 'n'})
	if a.playFile != 1 {
		t.Errorf("n from the last SNDH subtune should reach the YM file, playing %d", a.playFile)
	}
}

func TestYMExportWAV(t *testing.T) {
	f, err := parseTune("x.ym", tinyYM(25))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ym.wav")
	if err := exportWAV(path, f, 1, f.Duration(1), 8000, panMono.panning(), nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got, want := len(data)-44, 4000*4; got != want {
		t.Errorf("%d data bytes, want %d (0.5 s)", got, want)
	}
}

func TestParseTuneErrors(t *testing.T) {
	if _, err := parseTune("bad.ym", []byte("YM9!garbage")); err == nil || !strings.Contains(err.Error(), "ym:") {
		t.Errorf("bad .ym file: %v, want the YM parser's error", err)
	}
	if _, err := parseTune("bad.sndh", []byte("not a tune at all")); err == nil || !strings.Contains(err.Error(), "SNDH") {
		t.Errorf("bad .sndh file: %v, want the SNDH parser's error", err)
	}
	if !isTuneName("X.YM") || !isTuneName("a.SnDh") || isTuneName("a.txt") {
		t.Error("isTuneName")
	}
}
