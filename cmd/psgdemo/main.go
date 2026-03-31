package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"time"

	ym2149 "ym2149/emulation"
	"ym2149/internal/psgdemo"
	"ym2149/renderer/atarist"
	"ym2149/renderer/bandlimited"
	"ym2149/renderer/ebitenaudio"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const demoTPS = 60

type demoMode string

const (
	modeScript           demoMode = "script"
	modeInteractive      demoMode = "interactive"
	demoSampleRate                = 48_000
	demoOversampleFactor          = 4
)

type demoGame struct {
	mode demoMode

	chip   *ym2149.Chip
	reader *ebitenaudio.Reader
	player interface{ IsPlaying() bool }

	tickRemainder int
	ticks         int

	sequence *psgdemo.Sequencer
	control  interactiveState
}

func main() {
	modeFlag := flag.String("mode", string(modeScript), "demo mode: script or interactive")
	flag.Parse()

	mode := demoMode(*modeFlag)
	if mode != modeScript && mode != modeInteractive {
		log.Fatalf("unsupported mode %q", mode)
	}

	chip := ym2149.New(ym2149.Config{
		ClockHz:          2_000_000,
		OutputSampleRate: demoSampleRate * demoOversampleFactor,
		BufferSamples:    4_096 * demoOversampleFactor,
	})
	decimator, err := bandlimited.New(chip, bandlimited.Config{
		OversampleFactor: demoOversampleFactor,
	})
	if err != nil {
		log.Fatal(err)
	}
	boardOut := atarist.New(decimator, atarist.Config{})
	player, reader, err := ebitenaudio.NewPlayer(boardOut, 20*time.Millisecond)
	if err != nil {
		log.Fatal(err)
	}
	player.Play()

	game := &demoGame{
		mode:     mode,
		chip:     chip,
		reader:   reader,
		player:   player,
		sequence: psgdemo.NewSequencer(psgdemo.DefaultSequence()),
		control:  defaultInteractiveState(),
	}

	if mode == modeScript {
		game.sequence.Reset(game.chip)
	} else {
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
	status := fmt.Sprintf(
		"YM2149 demo\n\nMode: %s (Tab toggles)\nCycles: %d\nBuffered mono samples: %d\nAudio underruns: %d\nPlayer active: %t\nPort A in/out: %02x / %02x\nPort B in/out: %02x / %02x\n",
		g.mode,
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
