package ym

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestExternalYMCorpus validates the LZH depacker and parser against real .ym
// files plus reference decompressions produced by an independent tool
// (Python lhafile). Point YM_CORPUS_DIR at a directory holding <name>.ym and
// matching <name>.ym.ref files.
//
//	YM_CORPUS_DIR=/path/to/corpus go test ./format/ym/ -run ExternalYMCorpus -v
func TestExternalYMCorpus(t *testing.T) {
	dir := os.Getenv("YM_CORPUS_DIR")
	if dir == "" {
		t.Skip("set YM_CORPUS_DIR to run")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.ym"))
	if len(files) == 0 {
		t.Fatalf("no .ym files in %s", dir)
	}

	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			ref, refErr := os.ReadFile(path + ".ref")

			got, err := maybeDepack(raw)
			if err != nil {
				t.Fatalf("maybeDepack: %v", err)
			}
			if refErr == nil {
				n := min(len(got), len(ref))
				if !bytes.Equal(got[:n], ref[:n]) {
					for i := range n {
						if got[i] != ref[i] {
							t.Fatalf("depack differs at byte %d: got %#x want %#x", i, got[i], ref[i])
						}
					}
				}
				if len(got) != len(ref) {
					t.Logf("length: got %d, reference %d (header original-size field: %d)",
						len(got), len(ref), leU32(raw[11:15]))
				}
			}

			song, err := Parse(raw)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			t.Logf("YM%d %q by %q: %d frames, %d Hz, %d drums, %.1fs",
				song.Version, song.Name, song.Author, len(song.Frames),
				song.FrameHz, len(song.Drums), song.Duration())

			p, err := NewPlayer(song, PlayerConfig{SampleRate: 44100, Loop: false})
			if err != nil {
				t.Fatal(err)
			}
			var total int
			buf := make([]float32, 4096)
			for !p.Finished() {
				n := p.DrainMonoF32(buf)
				total += n
				if n == 0 {
					break
				}
			}
			wantSamples := len(song.Frames) * 44100 / song.FrameHz
			if total < wantSamples-2000 || total > wantSamples+2000 {
				t.Fatalf("rendered %d samples, want ~%d", total, wantSamples)
			}
		})
	}
}

func leU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
