// Command sndplayer plays SNDH and YM music files in the terminal, with per-voice
// oscilloscopes, a subtune list, song details, seeking, play modes and WAV
// export.
//
//	sndplayer [flags] file.sndh|file.ym|dir|archive.zip ...
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ebitengine/oto/v3"
)

func main() {
	rate := flag.Int("rate", 48_000, "output sample rate in Hz")
	modeFlag := flag.String("mode", "continuous", "play mode: single, loop, continuous or random")
	stereoFlag := flag.String("stereo", "abc", "channel panning: abc, acb or mono")
	subtune := flag.Int("subtune", 0, "subtune of the first file to start with (default: the file's default)")
	length := flag.Duration("length", 3*time.Minute, "play time for subtunes without a length in the header")
	wav := flag.String("wav", "", "render the first file's subtune to this WAV file and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sndplayer [flags] file.sndh|file.ym|directory|archive.zip ...\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nkeys: space pause · ←/→ seek 5s · </> seek 30s · 0-9 jump · tab focus · ↑/↓ select · enter play\n"+
			"      n/p next/prev subtune · N/P next/prev file · r random · m mode · s stereo · w export WAV · q quit\n")
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*rate, *modeFlag, *stereoFlag, *subtune, *length, *wav); err != nil {
		fmt.Fprintln(os.Stderr, "sndplayer:", err)
		os.Exit(1)
	}
}

func run(rate int, modeName, stereoName string, subtune int, length time.Duration, wav string) error {
	mode, err := parseMode(modeName)
	if err != nil {
		return err
	}
	pan, err := parsePan(stereoName)
	if err != nil {
		return err
	}
	list, err := buildPlaylist(flag.Args())
	if err != nil {
		return err
	}

	if wav != "" {
		return renderWAV(list[0], subtune, length, rate, pan, wav)
	}

	eng := newEngine(rate, mode, pan, length)
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   rate,
		ChannelCount: 2,
		Format:       oto.FormatFloat32LE,
		BufferSize:   40 * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("audio: %w", err)
	}
	<-ready
	out := ctx.NewPlayer(eng)
	out.SetBufferSize(rate * 8 * 60 / 1000) // 60 ms of float32 stereo
	out.Play()
	defer out.Close()

	a := newApp(eng, list, rate)
	if err := a.play(0, subtune); err != nil {
		a.say(stError, "%v", err)
	}

	t, err := openTerminal()
	if err != nil {
		return fmt.Errorf("terminal: %w", err)
	}
	defer t.restore()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		t.restore()
		os.Exit(1)
	}()

	a.run(t)
	return nil
}

func renderWAV(e *entry, subtune int, length time.Duration, rate int, pan panMode, path string) error {
	f, err := e.parse()
	if err != nil {
		return fmt.Errorf("%s: %w", e.name, err)
	}
	if subtune == 0 {
		subtune = f.DefaultSubtune
	}
	if subtune < 1 || subtune > f.Subtunes {
		return fmt.Errorf("%s has %d subtune(s)", e.name, f.Subtunes)
	}
	if d := f.Duration(subtune); d > 0 {
		length = d
	}
	fmt.Fprintf(os.Stderr, "%s – %s, subtune %d, %s → %s\n", clean(f.Composer), clean(f.Title), subtune, fmtDur(length), path)
	start := time.Now()
	last := -1
	err = exportWAV(path, f, subtune, length, rate, pan.panning(), func(p float64) {
		if pct := int(p * 100); pct != last {
			last = pct
			fmt.Fprintf(os.Stderr, "\r%3d%%", pct)
		}
	})
	fmt.Fprintf(os.Stderr, "\r%v\n", time.Since(start).Round(time.Millisecond))
	return err
}
