package sndh

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// rawSNDH builds an image whose three entry points branch past the header.
func rawSNDH(tags string) []byte {
	hdr := []byte("SNDH" + tags + "HDNS")
	if len(hdr)%2 != 0 {
		hdr = append(hdr, 0)
	}
	code := 12 + len(hdr)
	img := make([]byte, 0, code+2)
	for off := 0; off < 12; off += 4 {
		img = binary.BigEndian.AppendUint16(img, 0x6000) // bra.w
		img = binary.BigEndian.AppendUint16(img, uint16(code-off-2))
	}
	img = append(img, hdr...)
	return binary.BigEndian.AppendUint16(img, 0x4e75) // rts
}

func TestParseTags(t *testing.T) {
	names := "#!SN" + string([]byte{0, 6, 0, 10, 0, 14}) + "One\x00Two\x00Six\x00\x00"
	frms := "FRMS" + string([]byte{0, 0, 0x0b, 0xb8, 0, 0, 0, 0, 0, 0, 0, 100})
	f, err := Parse(rawSNDH("TITLLed Storm\x00COMMTim Follin\x00RIPPMr Hacker\x00CONVWho Knows\x00" +
		"YEAR1988\x00##03#!02TA120\x00FLAG~ye\x00" + names + frms))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct{ name, got, want string }{
		{"Title", f.Title, "Led Storm"},
		{"Composer", f.Composer, "Tim Follin"},
		{"Ripper", f.Ripper, "Mr Hacker"},
		{"Converter", f.Converter, "Who Knows"},
		{"Year", f.Year, "1988"},
		{"Flags", f.Flags, "ye"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if f.Subtunes != 3 || f.DefaultSubtune != 2 {
		t.Errorf("subtunes %d default %d, want 3 and 2", f.Subtunes, f.DefaultSubtune)
	}
	if f.Replay != (Replay{TimerA, 120}) {
		t.Errorf("Replay = %v, want Timer A 120 Hz", f.Replay)
	}
	if !slices.Equal(f.SubtuneNames, []string{"One", "Two", "Six"}) {
		t.Errorf("SubtuneNames = %q", f.SubtuneNames)
	}
	if !slices.Equal(f.Frames, []uint32{3000, 0, 100}) {
		t.Errorf("Frames = %v", f.Frames)
	}
	if d := f.Duration(1); d != 25*time.Second {
		t.Errorf("Duration(1) = %v, want 25s at 120 Hz", d)
	}
	if f.Duration(2) != 0 || f.Duration(9) != 0 {
		t.Error("unknown durations should be 0")
	}
	if !f.HasFlag('e') || f.HasFlag('s') {
		t.Error("HasFlag mismatch")
	}
	if f.Packed {
		t.Error("Packed set on a plain file")
	}
}

func TestParseDefaults(t *testing.T) {
	f, err := Parse(rawSNDH("TITLMinimal\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Subtunes != 1 || f.DefaultSubtune != 1 || f.Replay != (Replay{TimerC, 50}) {
		t.Errorf("defaults: %d subtunes, default %d, %v", f.Subtunes, f.DefaultSubtune, f.Replay)
	}
	if f.Frames != nil || f.Duration(1) != 0 {
		t.Error("no length expected")
	}
}

func TestParseVBLAndTime(t *testing.T) {
	f, err := Parse(rawSNDH("##02!V50TIME" + string([]byte{0, 30, 0, 0})))
	if err != nil {
		t.Fatal(err)
	}
	if f.Replay != (Replay{TimerVBL, 50}) {
		t.Errorf("Replay = %v", f.Replay)
	}
	if !slices.Equal(f.Frames, []uint32{1500, 0}) {
		t.Errorf("Frames = %v, want TIME seconds at 50 Hz", f.Frames)
	}
}

func TestParseSubtuneNamesRelative(t *testing.T) {
	// The format document's own example stores each name offset relative to
	// the previous name.
	names := "#!SN" + string([]byte{0, 4, 0, 6}) + "Hello\x00World\x00"
	f, err := Parse(rawSNDH("##02" + names))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.SubtuneNames, []string{"Hello", "World"}) {
		t.Errorf("SubtuneNames = %q", f.SubtuneNames)
	}
}

func TestParseStopsAtCode(t *testing.T) {
	// Tag-like bytes in the driver must not be read as header tags.
	img := rawSNDH("TITLx\x00")
	img = append(img, []byte("TC99\x00COMMbogus\x00")...)
	f, err := Parse(img)
	if err != nil {
		t.Fatal(err)
	}
	if f.Composer != "" || f.Replay.Hz != 50 {
		t.Errorf("read tags from code: composer %q, %v", f.Composer, f.Replay)
	}
}

func TestParseRejectsNonSNDH(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("short"), make([]byte, 64)} {
		if _, err := Parse(data); err == nil {
			t.Errorf("accepted %q", data)
		}
	}
}

// TestExternalSNDHCorpus parses and briefly plays real SNDH files. Point
// SNDH_CORPUS_DIR at a directory tree holding .sndh or .snd files.
//
//	SNDH_CORPUS_DIR=/path/to/sndh go test ./format/sndh/ -run ExternalSNDHCorpus -v
func TestExternalSNDHCorpus(t *testing.T) {
	dir := os.Getenv("SNDH_CORPUS_DIR")
	if dir == "" {
		t.Skip("set SNDH_CORPUS_DIR to run")
	}
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		ext := strings.ToLower(filepath.Ext(path))
		if err == nil && !d.IsDir() && (ext == ".sndh" || ext == ".snd") {
			files = append(files, path)
		}
		return nil
	})
	if len(files) == 0 {
		t.Fatalf("no .sndh/.snd files under %s", dir)
	}

	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewPlayerFromBytes(raw, PlayerConfig{SampleRate: 22050})
			if err != nil {
				t.Fatal(err)
			}
			pcm := render(p, 3*time.Second)
			if p.Err() != nil {
				t.Fatal(p.Err())
			}
			f := p.File()
			t.Logf("%q by %q (%s): %d subtune(s), %v, %v, peak-to-peak %.2f",
				f.Title, f.Composer, f.Year, f.Subtunes, f.Replay, p.Duration(), peakToPeak(pcm))
		})
	}
}
