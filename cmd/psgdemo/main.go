package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"time"

	ym2149 "github.com/jenska/ym2149/emulation"

	"github.com/jenska/ym2149/format/sndh"
	"github.com/jenska/ym2149/format/ym"
	"github.com/jenska/ym2149/internal/psgdemo"
	"github.com/jenska/ym2149/renderer/atarist"
	"github.com/jenska/ym2149/renderer/bandlimited"
	"github.com/jenska/ym2149/renderer/ebitenaudio"
	"github.com/jenska/ym2149/renderer/stereo"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const demoTPS = 60

type demoMode string

const (
	modeScript           demoMode = "script"
	modeInteractive      demoMode = "interactive"
	modeFile             demoMode = "file"
	demoSampleRate                = 48_000
	demoOversampleFactor          = 4
)

type underrunReporter interface{ Underruns() uint64 }

// tunePlayer is what the demo needs from a music-file player (ym or sndh).
type tunePlayer interface {
	DrainMonoF32([]float32) int
	DrainChannelF32(int, []float32) int
	OutputSampleRate() int
	Chip() *ym2149.Chip
}

type demoGame struct {
	mode   demoMode
	stereo bool

	chip   *ym2149.Chip
	reader underrunReporter
	player interface{ IsPlaying() bool }

	tickRemainder int
	ticks         int

	sequence *psgdemo.Sequencer
	control  interactiveState

	tune     tunePlayer
	ymTune   *ym.Player
	sndhTune *sndh.Player
}

func main() {
	modeFlag := flag.String("mode", string(modeScript), "demo mode: script or interactive")
	fileFlag := flag.String("file", "", "play a YM (.ym) or SNDH (.sndh) music file instead of the built-in demo")
	subtuneFlag := flag.Int("subtune", 0, "SNDH subtune to play (default: the file's default subtune)")
	stereoFlag := flag.Bool("stereo", false, "pan the three channels A-left / B-centre / C-right")
	flag.Parse()

	mode := demoMode(*modeFlag)
	if *fileFlag != "" {
		mode = modeFile
	}
	if mode != modeScript && mode != modeInteractive && mode != modeFile {
		log.Fatalf("unsupported mode %q", mode)
	}

	const chipRate = demoSampleRate * demoOversampleFactor

	chip := ym2149.New(ym2149.Config{
		ClockHz:          2_000_000,
		OutputSampleRate: chipRate,
		BufferSamples:    4_096 * demoOversampleFactor,
		ChannelTaps:      *stereoFlag,
	})

	var (
		tune     tunePlayer
		ymTune   *ym.Player
		sndhTune *sndh.Player
	)
	if mode == modeFile {
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			log.Fatal(err)
		}
		if f, perr := sndh.Parse(data); perr == nil {
			sndhTune, err = sndh.NewPlayer(f, sndh.PlayerConfig{
				SampleRate:  chipRate,
				Subtune:     *subtuneFlag,
				Loop:        true,
				ChannelTaps: *stereoFlag,
			})
			tune = sndhTune
		} else {
			ymTune, err = ym.NewPlayerFromBytes(data, ym.PlayerConfig{
				SampleRate:  chipRate,
				Loop:        true,
				ChannelTaps: *stereoFlag,
			})
			tune = ymTune
		}
		if err != nil {
			log.Fatalf("%s: %v", *fileFlag, err)
		}
		chip = tune.Chip()
	}

	// A mono board-output stage: source -> bandlimited decimation -> ST filter.
	board := func(src bandlimited.MonoSource) *atarist.Output {
		dec, err := bandlimited.New(src, bandlimited.Config{OversampleFactor: demoOversampleFactor})
		if err != nil {
			log.Fatal(err)
		}
		return atarist.New(dec, atarist.Config{})
	}

	var (
		audioPlayer *audio.Player
		reader      underrunReporter
		err         error
	)
	if *stereoFlag {
		var chSrc stereo.ChannelSource = chip
		if tune != nil {
			chSrc = tune
		}
		split := stereo.NewSplitter(chSrc, stereo.ABC())
		out := stereo.Interleave(board(split.Left()), board(split.Right()))
		audioPlayer, reader, err = ebitenaudio.NewStereoPlayer(out, 20*time.Millisecond)
	} else {
		var src bandlimited.MonoSource = chip
		if tune != nil {
			src = tune
		}
		audioPlayer, reader, err = ebitenaudio.NewPlayer(board(src), 20*time.Millisecond)
	}
	if err != nil {
		log.Fatal(err)
	}
	audioPlayer.Play()

	game := &demoGame{
		mode:     mode,
		stereo:   *stereoFlag,
		chip:     chip,
		reader:   reader,
		player:   audioPlayer,
		sequence: psgdemo.NewSequencer(psgdemo.DefaultSequence()),
		control:  defaultInteractiveState(),
		tune:     tune,
		ymTune:   ymTune,
		sndhTune: sndhTune,
	}

	switch mode {
	case modeScript:
		game.sequence.Reset(game.chip)
	case modeInteractive:
		game.control.apply(game.chip)
	}

	ebiten.SetWindowTitle("YM2149 PSG Demo")
	ebiten.SetTPS(demoTPS)
	ebiten.SetWindowSize(800, 480)
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}

func (g *demoGame) Update() error {
	if g.mode == modeFile {
		// The ym.Player advances its own chip as the audio backend drains it.
		return nil
	}

	switch g.mode {
	case modeScript:
		g.sequence.Tick(g.chip)
	case modeInteractive:
		g.control.updateFromKeyboard()
		g.control.apply(g.chip)
	}

	g.tickRemainder += g.chip.ClockHz()
	cycles := g.tickRemainder / demoTPS
	g.tickRemainder %= demoTPS
	g.chip.Step(uint32(cycles))
	g.ticks++

	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		if g.mode == modeScript {
			g.mode = modeInteractive
			g.control.apply(g.chip)
		} else {
			g.mode = modeScript
			g.sequence.Reset(g.chip)
		}
	}
	return nil
}

func (g *demoGame) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 21, G: 24, B: 28, A: 255})

	ports := g.chip.Ports()
	outMode := "mono"
	if g.stereo {
		outMode = "stereo (A<L  B·  C>R)"
	}
	status := fmt.Sprintf(
		"YM2149 demo\n\nMode: %s (Tab toggles)\nOutput: %s\nCycles: %d\nBuffered mono samples: %d\nAudio underruns: %d\nPlayer active: %t\nPort A in/out: %02x / %02x\nPort B in/out: %02x / %02x\n",
		g.mode,
		outMode,
		g.chip.Cycles(),
		g.chip.BufferedSamples(),
		g.reader.Underruns(),
		g.player.IsPlaying(),
		ports.AInput,
		ports.AOutput,
		ports.BInput,
		ports.BOutput,
	)

	switch g.mode {
	case modeFile:
		if g.sndhTune != nil {
			status += sndhStatus(g.sndhTune)
			break
		}
		s := g.ymTune.Song()
		status += fmt.Sprintf(
			"\nNow playing: %s\nAuthor: %s\nYM v%d  %d Hz frames  %d kHz clock\nFrame %d / %d\n%s\n",
			nonEmpty(s.Name, "(untitled)"),
			nonEmpty(s.Author, "(unknown)"),
			s.Version, s.FrameHz, s.ClockHz/1000,
			g.ymTune.Frame(), g.ymTune.TotalFrames(),
			s.Comment,
		)
	case modeScript:
		status += fmt.Sprintf("\nScript step: %s\n", g.sequence.CurrentName())
		status += "Scripted sequence sweeps tone, envelope, and noise.\n"
	case modeInteractive:
		status += fmt.Sprintf(
			"\nTone period: %d\nNoise period: %d\nVolume: %d\nEnvelope: %t\nShape: 0x%x\nTone enabled: %t\nNoise enabled: %t\n",
			g.control.tonePeriod,
			g.control.noisePeriod,
			g.control.volume,
			g.control.envelope,
			g.control.shape,
			g.control.toneEnabled,
			g.control.noiseEnabled,
		)
		status += "Arrows: tone period / volume\nQ/A: noise period\nT: tone toggle  N: noise toggle  E: envelope toggle  [/]: shape\n"
	}

	ebitenutil.DebugPrint(screen, status)
}

func (g *demoGame) Layout(_, _ int) (int, int) {
	return 800, 480
}

func sndhStatus(p *sndh.Player) string {
	f := p.File()
	length := "unknown"
	if d := p.Duration(); d > 0 {
		length = d.Round(time.Second).String()
	}
	s := fmt.Sprintf(
		"\nNow playing: %s\nComposer: %s\nYear: %s  Ripper: %s\nSNDH subtune %d / %d  replay %v %d Hz\nTime %s / %s\n",
		nonEmpty(f.Title, "(untitled)"),
		nonEmpty(f.Composer, "(unknown)"),
		nonEmpty(f.Year, "?"), nonEmpty(f.Ripper, "?"),
		p.Subtune(), f.Subtunes, f.Replay.Timer, f.Replay.Hz,
		p.Position().Round(time.Second), length,
	)
	if names := f.SubtuneNames; len(names) >= p.Subtune() {
		s += names[p.Subtune()-1] + "\n"
	}
	if err := p.Err(); err != nil {
		s += fmt.Sprintf("Stopped: %v\n", err)
	}
	return s
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

type interactiveState struct {
	tonePeriod   uint16
	noisePeriod  byte
	volume       byte
	shape        byte
	envelope     bool
	toneEnabled  bool
	noiseEnabled bool
}

func defaultInteractiveState() interactiveState {
	return interactiveState{
		tonePeriod:   256,
		noisePeriod:  8,
		volume:       12,
		shape:        0x0d,
		toneEnabled:  true,
		noiseEnabled: false,
	}
}

func (s *interactiveState) updateFromKeyboard() {
	if inpututil.IsKeyJustPressed(ebiten.KeyLeft) && s.tonePeriod > 1 {
		s.tonePeriod--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRight) && s.tonePeriod < 0x0fff {
		s.tonePeriod++
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyUp) && s.volume < 15 {
		s.volume++
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) && s.volume > 0 {
		s.volume--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyQ) && s.noisePeriod > 1 {
		s.noisePeriod--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyA) && s.noisePeriod < 31 {
		s.noisePeriod++
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyT) {
		s.toneEnabled = !s.toneEnabled
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyN) {
		s.noiseEnabled = !s.noiseEnabled
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyE) {
		s.envelope = !s.envelope
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyLeftBracket) {
		s.shape = (s.shape - 1) & 0x0f
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRightBracket) {
		s.shape = (s.shape + 1) & 0x0f
	}
}

func (s interactiveState) apply(chip *ym2149.Chip) {
	psgdemo.WriteReg(chip, 0, byte(s.tonePeriod))
	psgdemo.WriteReg(chip, 1, byte(s.tonePeriod>>8))
	psgdemo.WriteReg(chip, 6, s.noisePeriod)

	mixer := byte(0x3f)
	if s.toneEnabled {
		mixer &^= 0x01
	}
	if s.noiseEnabled {
		mixer &^= 0x08
	}
	psgdemo.WriteReg(chip, 7, mixer)

	vol := s.volume & 0x0f
	if s.envelope {
		vol |= 0x10
	}
	psgdemo.WriteReg(chip, 8, vol)
	psgdemo.WriteReg(chip, 9, 0)
	psgdemo.WriteReg(chip, 10, 0)
	psgdemo.WriteReg(chip, 11, 0x02)
	psgdemo.WriteReg(chip, 12, 0x00)
	psgdemo.WriteReg(chip, 13, s.shape&0x0f)
}
