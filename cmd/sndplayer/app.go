package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

type focus int

const (
	focusFiles focus = iota
	focusSubtunes
)

type app struct {
	eng  *engine
	list []*entry
	rate int

	focus            focus
	fileSel, fileTop int
	subSel, subTop   int
	playFile         int // -1 while nothing was started
	playSub          int // 1-based

	lay layout

	msg      string
	msgStyle style
	msgUntil time.Time

	exporting  atomic.Bool
	exportProg atomic.Uint64 // float64 bits
	exportFile string
	exportDone chan error

	rng *rand.Rand
}

func newApp(eng *engine, list []*entry, rate int) *app {
	return &app{
		eng: eng, list: list, rate: rate, playFile: -1,
		exportDone: make(chan error, 1),
		rng:        rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x5d)),
	}
}

func (a *app) say(st style, format string, args ...any) {
	a.msg, a.msgStyle = fmt.Sprintf(format, args...), st
	a.msgUntil = time.Now().Add(4 * time.Second)
}

func (a *app) message() (string, style) {
	if a.exporting.Load() {
		p := math.Float64frombits(a.exportProg.Load())
		return fmt.Sprintf("Exporting %s … %3.0f%%", a.exportFile, 100*p), stValue
	}
	if time.Now().Before(a.msgUntil) {
		return a.msg, a.msgStyle
	}
	return "", stNormal
}

func (a *app) playingFile() (*tune, error) {
	if a.playFile < 0 {
		return nil, nil
	}
	return a.list[a.playFile].parse()
}

// play starts subtune sub (0 = default) of playlist entry idx and moves the
// selection to it.
func (a *app) play(idx, sub int) error {
	f, err := a.list[idx].parse()
	if err != nil {
		return fmt.Errorf("%s: %w", a.list[idx].name, err)
	}
	if sub == 0 {
		sub = f.DefaultSubtune
	}
	if err := a.eng.load(f, sub); err != nil {
		return fmt.Errorf("%s: %w", a.list[idx].name, err)
	}
	a.playFile, a.playSub = idx, sub
	a.fileSel, a.subSel = idx, sub-1
	return nil
}

// step plays the next (dir=1) or previous (dir=-1) subtune, crossing file
// boundaries. Unloadable files are skipped.
func (a *app) step(dir int) {
	if a.playFile < 0 {
		a.tryPlay(a.fileSel, 0)
		return
	}
	idx, sub := a.playFile, a.playSub+dir
	for range len(a.list) + 1 {
		f, err := a.list[idx].parse()
		if err == nil && sub >= 1 && sub <= f.Subtunes {
			if a.play(idx, sub) == nil {
				return
			}
		}
		idx = (idx + dir + len(a.list)) % len(a.list)
		sub = 1
		if dir < 0 {
			if g, err := a.list[idx].parse(); err == nil {
				sub = g.Subtunes
			}
		}
	}
	a.say(stError, "nothing playable in the playlist")
}

func (a *app) stepFile(dir int) {
	idx := a.fileSel
	if a.playFile >= 0 {
		idx = a.playFile
	}
	for range len(a.list) {
		idx = (idx + dir + len(a.list)) % len(a.list)
		if a.play(idx, 0) == nil {
			return
		}
	}
	a.say(stError, "nothing playable in the playlist")
}

func (a *app) random() {
	for range 20 {
		idx := a.rng.IntN(len(a.list))
		f, err := a.list[idx].parse()
		if err != nil {
			continue
		}
		if a.play(idx, 1+a.rng.IntN(f.Subtunes)) == nil {
			return
		}
	}
	a.say(stError, "no playable tune found")
}

func (a *app) tryPlay(idx, sub int) {
	if err := a.play(idx, sub); err != nil {
		a.say(stError, "%v", err)
	}
}

// tuneEnded applies the play mode once the current tune has finished.
func (a *app) tuneEnded() {
	switch a.eng.snapshot(0).mode {
	case modeContinuous:
		a.step(1)
	case modeRandom:
		a.random()
	}
}

func (a *app) seekBy(d time.Duration) {
	st := a.eng.snapshot(0)
	if !st.loaded {
		return
	}
	if err := a.eng.seek(st.position + d); err != nil {
		a.say(stError, "seek: %v", err)
	}
}

func (a *app) seekFrac(frac float64) {
	st := a.eng.snapshot(0)
	if !st.loaded || st.length == 0 {
		return
	}
	if err := a.eng.seek(time.Duration(frac * float64(st.length))); err != nil {
		a.say(stError, "seek: %v", err)
	}
}

func (a *app) moveSel(delta int) {
	if a.focus == focusFiles {
		a.fileSel = max(0, min(len(a.list)-1, a.fileSel+delta))
		a.subSel, a.subTop = 0, 0
		return
	}
	if f, err := a.list[a.fileSel].parse(); err == nil {
		a.subSel = max(0, min(f.Subtunes-1, a.subSel+delta))
	}
}

func (a *app) activate() {
	if a.focus == focusFiles {
		a.tryPlay(a.fileSel, 0)
	} else {
		a.tryPlay(a.fileSel, a.subSel+1)
	}
}

func (a *app) startExport() {
	if a.exporting.Load() || a.playFile < 0 {
		return
	}
	f, err := a.playingFile()
	if err != nil {
		return
	}
	st := a.eng.snapshot(0)
	length := st.duration
	if length == 0 {
		length = a.eng.defaultLength
	}
	base := strings.TrimSuffix(filepath.Base(a.list[a.playFile].name), filepath.Ext(a.list[a.playFile].name))
	a.exportFile = fmt.Sprintf("%s-%d.wav", base, a.playSub)
	a.exportProg.Store(0)
	a.exporting.Store(true)
	path, sub, pan := a.exportFile, a.playSub, st.pan.panning()
	go func() {
		err := exportWAV(path, f, sub, length, a.rate, pan, func(p float64) {
			a.exportProg.Store(math.Float64bits(p))
		})
		a.exporting.Store(false)
		a.exportDone <- err
	}()
}

// handle reacts to one input event; it returns false to quit.
func (a *app) handle(ev event) bool {
	switch ev.key {
	case keyQuit:
		return false
	case keyUp:
		a.moveSel(-1)
	case keyDown:
		a.moveSel(1)
	case keyPgUp:
		a.moveSel(-10)
	case keyPgDn:
		a.moveSel(10)
	case keyHome:
		a.moveSel(-1 << 30)
	case keyEnd:
		a.moveSel(1 << 30)
	case keyTab:
		a.focus = 1 - a.focus
	case keyEnter:
		a.activate()
	case keyLeft:
		a.seekBy(-5 * time.Second)
	case keyRight:
		a.seekBy(5 * time.Second)
	case keyRune:
		switch ev.r {
		case ' ':
			if a.playFile < 0 {
				a.activate()
			} else {
				a.eng.togglePause()
			}
		case '<', ',':
			a.seekBy(-30 * time.Second)
		case '>', '.':
			a.seekBy(30 * time.Second)
		case 'n':
			a.step(1)
		case 'p':
			a.step(-1)
		case 'N':
			a.stepFile(1)
		case 'P':
			a.stepFile(-1)
		case 'r':
			a.random()
		case 'm':
			m := (a.eng.snapshot(0).mode + 1) % numModes
			a.eng.setMode(m)
			a.say(stValue, "Play mode: %s", m)
		case 's':
			p := (a.eng.snapshot(0).pan + 1) % numPans
			a.eng.setPan(p)
			a.say(stValue, "Stereo: %s", p)
		case 'w':
			a.startExport()
		case 'q', 'Q':
			return false
		default:
			if ev.r >= '0' && ev.r <= '9' {
				a.seekFrac(float64(ev.r-'0') / 10)
			}
		}
	case keyClick:
		a.click(ev.x, ev.y)
	case keyWheelUp:
		a.moveSel(-3)
	case keyWheelDown:
		a.moveSel(3)
	}
	return true
}

// click handles a left mouse click at 0-based cell (x, y): seek on the time
// bar, select in a list, and play when clicking the selected row again.
func (a *app) click(x, y int) {
	if y == a.lay.barY && x >= a.lay.barX && x < a.lay.barX+a.lay.barW {
		a.seekFrac(float64(x-a.lay.barX) / float64(max(1, a.lay.barW-1)))
		return
	}
	if row, ok := a.lay.files.hit(x, y); ok {
		idx := a.fileTop + row
		if idx < len(a.list) {
			if a.focus == focusFiles && idx == a.fileSel {
				a.activate()
			} else if idx != a.fileSel {
				a.fileSel, a.subSel, a.subTop = idx, 0, 0
			}
			a.focus = focusFiles
		}
		return
	}
	if row, ok := a.lay.subs.hit(x, y); ok {
		f, err := a.list[a.fileSel].parse()
		if idx := a.subTop + row; err == nil && idx < f.Subtunes {
			if a.focus == focusSubtunes && idx == a.subSel {
				a.subSel = idx
				a.activate()
			}
			a.subSel = idx
			a.focus = focusSubtunes
		}
	}
}

// run is the UI loop: draw at ~30 fps and react to input, tune ends and
// finished exports.
func (a *app) run(term *terminal) {
	tick := time.NewTicker(33 * time.Millisecond)
	defer tick.Stop()
	w, h := term.size()
	for {
		if nw, nh := term.size(); nw != w || nh != h {
			w, h = nw, nh
			term.clear()
		}
		os.Stdout.WriteString(a.draw(w, h).render(term.color))

		select {
		case ev, ok := <-term.events:
			if !ok || !a.handle(ev) {
				return
			}
		case <-a.eng.endCh:
			a.tuneEnded()
		case err := <-a.exportDone:
			if err != nil {
				a.say(stError, "WAV export failed: %v", err)
			} else {
				a.say(stPlaying, "Wrote %s", a.exportFile)
			}
		case <-tick.C:
		}
	}
}
