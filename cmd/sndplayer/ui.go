package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type style uint8

const (
	stNormal style = iota
	stBorder
	stTitle
	stLabel
	stValue
	stHeader
	stSelected
	stSelectedDim
	stPlaying
	stBar
	stDim
	stError
	stVoiceA
	stVoiceB
	stVoiceC
)

var styleCodes = [...]string{
	stNormal:      "\x1b[0m",
	stBorder:      "\x1b[0;36m",
	stTitle:       "\x1b[0;1;33m",
	stLabel:       "\x1b[0;2m",
	stValue:       "\x1b[0;1m",
	stHeader:      "\x1b[0;1;30;46m",
	stSelected:    "\x1b[0;1;30;43m",
	stSelectedDim: "\x1b[0;7m",
	stPlaying:     "\x1b[0;1;32m",
	stBar:         "\x1b[0;1;36m",
	stDim:         "\x1b[0;2m",
	stError:       "\x1b[0;1;31m",
	stVoiceA:      "\x1b[0;1;32m",
	stVoiceB:      "\x1b[0;1;33m",
	stVoiceC:      "\x1b[0;1;35m",
}

// Styles that must stay visible without colour.
var monoCodes = map[style]string{
	stHeader: "\x1b[0;7m", stSelected: "\x1b[0;7m", stSelectedDim: "\x1b[0;7m",
	stTitle: "\x1b[0;1m", stValue: "\x1b[0;1m", stPlaying: "\x1b[0;1m", stError: "\x1b[0;1m",
}

type cell struct {
	r rune
	s style
}

// canvas is a grid of single-width cells rendered in one write per frame.
type canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([]cell, w*h)}
	for i := range c.cells {
		c.cells[i] = cell{' ', stNormal}
	}
	return c
}

func (c *canvas) set(x, y int, r rune, s style) {
	if x >= 0 && y >= 0 && x < c.w && y < c.h {
		c.cells[y*c.w+x] = cell{r, s}
	}
}

// text writes s at (x, y), clipped to max cells; it returns the cells used.
func (c *canvas) text(x, y int, s string, st style, max int) int {
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		c.set(x+n, y, r, st)
		n++
	}
	return n
}

func (c *canvas) fill(x, y, w int, r rune, st style) {
	for i := range w {
		c.set(x+i, y, r, st)
	}
}

func (c *canvas) box(x, y, w, h int, title string, st style) {
	if w < 2 || h < 2 {
		return
	}
	c.set(x, y, '╭', st)
	c.set(x+w-1, y, '╮', st)
	c.set(x, y+h-1, '╰', st)
	c.set(x+w-1, y+h-1, '╯', st)
	c.fill(x+1, y, w-2, '─', st)
	c.fill(x+1, y+h-1, w-2, '─', st)
	for i := 1; i < h-1; i++ {
		c.set(x, y+i, '│', st)
		c.set(x+w-1, y+i, '│', st)
	}
	if title != "" {
		c.text(x+2, y, " "+title+" ", stTitle, w-4)
	}
}

func (c *canvas) render(color bool) string {
	var sb strings.Builder
	sb.Grow(len(c.cells) * 4)
	sb.WriteString("\x1b[H")
	cur := style(255)
	for y := range c.h {
		if y > 0 {
			sb.WriteString("\r\n")
		}
		for x := range c.w {
			cl := c.cells[y*c.w+x]
			if cl.s != cur {
				cur = cl.s
				switch code, ok := monoCodes[cl.s]; {
				case color:
					sb.WriteString(styleCodes[cl.s])
				case ok:
					sb.WriteString(code)
				default:
					sb.WriteString("\x1b[0m")
				}
			}
			sb.WriteRune(cl.r)
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// --- widgets ---

// clean makes header text printable: control and non-ASCII bytes (Atari
// charset) become '?'.
func clean(s string) string {
	b := []rune(s)
	for i, r := range b {
		if r < 32 || r > 126 {
			b[i] = '?'
		}
	}
	return string(b)
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// Braille dot bits for a 2x4 cell, indexed [y][x].
var brailleBits = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

const (
	scopeSpan  = 1024 // samples shown per scope (~21 ms at 48 kHz)
	scopeRange = 0.6  // signal range mapped onto the scope height
	fullScale  = 0.53 // peak-to-peak of one voice at volume 15
)

// drawScope renders samples into the w x h cell area at (x, y) as a braille
// oscilloscope, triggered on a rising edge through the mean.
func (c *canvas) drawScope(x, y, w, h int, s []float32, st style) {
	if w <= 0 || h <= 0 || len(s) < 2*scopeSpan {
		return
	}
	var mean float64
	for _, v := range s {
		mean += float64(v)
	}
	mean /= float64(len(s))

	start := 0
	for i := range len(s) - scopeSpan - 1 {
		if float64(s[i]) <= mean && float64(s[i+1]) > mean {
			start = i
			break
		}
	}
	win := s[start : start+scopeSpan]

	cols, rows := 2*w, 4*h
	dots := make([]rune, w*h)
	row := func(v float32) int {
		r := int(math.Round((0.5 - (float64(v)-mean)/scopeRange) * float64(rows-1)))
		return max(0, min(rows-1, r))
	}
	prev := row(win[0])
	for dx := range cols {
		a, b := dx*len(win)/cols, max((dx+1)*len(win)/cols, dx*len(win)/cols+1)
		lo, hi := prev, prev
		for _, v := range win[a:b] {
			r := row(v)
			lo, hi = min(lo, r), max(hi, r)
		}
		for dy := lo; dy <= hi; dy++ {
			dots[(dy/4)*w+dx/2] |= brailleBits[dy%4][dx%2]
		}
		prev = row(win[b-1])
	}
	for i, d := range dots {
		r := ' '
		if d != 0 {
			r = 0x2800 + d
		}
		c.set(x+i%w, y+i/w, r, st)
	}
}

func level(s []float32) float64 {
	if len(s) < scopeSpan/2 {
		return 0
	}
	lo, hi := s[len(s)-scopeSpan/2], s[len(s)-scopeSpan/2]
	for _, v := range s[len(s)-scopeSpan/2:] {
		lo, hi = min(lo, v), max(hi, v)
	}
	return min(1, float64(hi-lo)/fullScale)
}

func (c *canvas) meter(x, y, w int, frac float64, st style) {
	full := int(math.Round(frac * float64(w)))
	for i := range w {
		if i < full {
			c.set(x+i, y, '█', st)
		} else {
			c.set(x+i, y, '░', stDim)
		}
	}
}

// --- the screen ---

// layout remembers where clickable things were drawn.
type layout struct {
	barX, barY, barW int
	files, subs      listArea
}

type listArea struct{ x, y, w, h int }

func (a listArea) hit(x, y int) (int, bool) {
	if x >= a.x && x < a.x+a.w && y >= a.y && y < a.y+a.h {
		return y - a.y, true
	}
	return 0, false
}

func (a *app) draw(w, h int) *canvas {
	c := newCanvas(w, h)
	if w < 60 || h < 20 {
		c.text(0, 0, fmt.Sprintf("Terminal too small (%dx%d), need 60x20.", w, h), stError, w)
		return c
	}
	st := a.eng.snapshot(2 * scopeSpan)

	// Header.
	c.fill(0, 0, w, ' ', stHeader)
	c.text(1, 0, "♫ sndplayer · SNDH player · YM2149 + 68000", stHeader, w-2)
	state := "■ stopped"
	switch {
	case !st.loaded:
	case st.err != nil:
		state = "✖ crashed"
	case st.ended:
		state = "■ finished"
	case st.paused:
		state = "❚❚ paused"
	default:
		state = "▶ playing"
	}
	right := fmt.Sprintf("%s · mode %s · stereo %s ", state, st.mode, st.pan)
	c.text(w-len([]rune(right)), 0, right, stHeader, w)

	// Song info.
	f, ferr := a.playingFile()
	c.box(0, 1, w, 6, "Song", stBorder)
	half := (w - 4) / 2
	field := func(x, y int, label, value string, width int) {
		n := c.text(x, y, label, stLabel, width)
		c.text(x+n, y, clean(value), stValue, width-n)
	}
	if f != nil {
		sub := a.playSub
		subName := ""
		if sub >= 1 && sub <= len(f.SubtuneNames) {
			subName = " · " + f.SubtuneNames[sub-1]
		}
		field(2, 2, "Title     ", f.Title, half)
		field(2, 3, "Composer  ", f.Composer, half)
		field(2, 4, "Ripper    ", f.Ripper, half)
		field(2, 5, "Converter ", f.Converter, half)
		field(2+half, 2, "Year    ", f.Year, w-half-4)
		field(2+half, 3, "Subtune ", fmt.Sprintf("%d of %d%s", sub, f.Subtunes, subName), w-half-4)
		field(2+half, 4, "Replay  ", fmt.Sprintf("%v at %d Hz", f.Replay.Timer, f.Replay.Hz), w-half-4)
		flags := f.Flags
		if f.Packed {
			flags = strings.TrimSpace(flags + " (ICE! packed)")
		}
		field(2+half, 5, "Flags   ", flags, w-half-4)
	} else if ferr != nil {
		c.text(2, 3, ferr.Error(), stError, w-4)
	} else {
		c.text(2, 3, "Nothing playing. Pick a file and press Enter.", stDim, w-4)
	}

	// Oscilloscopes.
	listMin := 5
	scopeH := max(5, min(12, h-7-1-1-listMin-2))
	y := 7
	voices := [3]style{stVoiceA, stVoiceB, stVoiceC}
	bw := w / 3
	for ch := range 3 {
		x0 := ch * bw
		ww := bw
		if ch == 2 {
			ww = w - x0
		}
		c.box(x0, y, ww, scopeH, fmt.Sprintf("Voice %c", 'A'+ch), stBorder)
		if st.loaded {
			c.drawScope(x0+1, y+1, ww-2, scopeH-3, st.scope[ch], voices[ch])
			c.meter(x0+2, y+scopeH-2, ww-4, level(st.scope[ch]), voices[ch])
		}
	}
	y += scopeH

	// Time bar.
	icon := "▶"
	if st.paused || st.ended || !st.loaded {
		icon = "❚❚"
	}
	pos := fmtDur(st.position)
	end := "∞"
	if st.length > 0 {
		end = fmtDur(st.length)
	}
	left := fmt.Sprintf(" %s %s ", icon, pos)
	c.text(0, y, left, stValue, w)
	bx := len([]rune(left))
	bwid := w - bx - len([]rune(end)) - 2
	a.lay.barX, a.lay.barY, a.lay.barW = bx, y, bwid
	head := -1
	if st.length > 0 {
		head = int(float64(bwid-1) * min(1, st.position.Seconds()/st.length.Seconds()))
	}
	for i := range bwid {
		switch {
		case i < head:
			c.set(bx+i, y, '━', stBar)
		case i == head:
			c.set(bx+i, y, '●', stBar)
		default:
			c.set(bx+i, y, '─', stDim)
		}
	}
	c.text(bx+bwid+1, y, end, stValue, w)
	y++

	// Lists.
	listH := h - y - 1
	fw := w * 3 / 5
	a.drawFiles(c, 0, y, fw, listH)
	a.drawSubtunes(c, fw, y, w-fw, listH)

	// Footer: message or key help.
	msg, msgStyle := a.message()
	if msg == "" {
		msg, msgStyle = "␣ pause  ←/→ seek  ⇥ focus  ↑↓ select  ⏎ play  n/p next/prev  m mode  s stereo  w wav  q quit", stDim
	}
	c.text(1, h-1, msg, msgStyle, w-2)
	return c
}

func (a *app) drawFiles(c *canvas, x, y, w, h int) {
	title := fmt.Sprintf("Files %d/%d", a.fileSel+1, len(a.list))
	st := stBorder
	if a.focus == focusFiles {
		st = stTitle
	}
	c.box(x, y, w, h, title, st)
	rows := h - 2
	a.lay.files = listArea{x + 1, y + 1, w - 2, rows}
	a.fileTop = scrollTop(a.fileTop, a.fileSel, rows)
	for i := range rows {
		idx := a.fileTop + i
		if idx >= len(a.list) {
			break
		}
		e := a.list[idx]
		rowSt := stNormal
		switch {
		case idx == a.fileSel && a.focus == focusFiles:
			rowSt = stSelected
		case idx == a.fileSel:
			rowSt = stSelectedDim
		case idx == a.playFile:
			rowSt = stPlaying
		}
		c.fill(x+1, y+1+i, w-2, ' ', rowSt)
		mark := "  "
		if idx == a.playFile {
			mark = "♪ "
		}
		label := mark + e.name
		if f, err := e.parse(); err != nil {
			label += " · unreadable"
		} else if f.Title != "" {
			label += " · " + clean(f.Title)
		}
		c.text(x+1, y+1+i, label, rowSt, w-3)
	}
}

func (a *app) drawSubtunes(c *canvas, x, y, w, h int) {
	st := stBorder
	if a.focus == focusSubtunes {
		st = stTitle
	}
	f, err := a.list[a.fileSel].parse()
	title := "Subtunes"
	if f != nil {
		title = fmt.Sprintf("Subtunes %d/%d", a.subSel+1, f.Subtunes)
	}
	c.box(x, y, w, h, title, st)
	rows := h - 2
	a.lay.subs = listArea{x + 1, y + 1, w - 2, rows}
	if err != nil {
		c.text(x+2, y+1, err.Error(), stError, w-4)
		return
	}
	a.subSel = min(a.subSel, f.Subtunes-1)
	a.subTop = scrollTop(a.subTop, a.subSel, rows)
	for i := range rows {
		idx := a.subTop + i
		if idx >= f.Subtunes {
			break
		}
		sub := idx + 1
		playing := a.fileSel == a.playFile && sub == a.playSub
		rowSt := stNormal
		switch {
		case idx == a.subSel && a.focus == focusSubtunes:
			rowSt = stSelected
		case idx == a.subSel:
			rowSt = stSelectedDim
		case playing:
			rowSt = stPlaying
		}
		c.fill(x+1, y+1+i, w-2, ' ', rowSt)
		mark := "  "
		if playing {
			mark = "♪ "
		}
		name := ""
		if idx < len(f.SubtuneNames) {
			name = clean(f.SubtuneNames[idx])
		}
		if sub == f.DefaultSubtune {
			name = strings.TrimSpace(name + " *")
		}
		dur := "--:--"
		if d := f.Duration(sub); d > 0 {
			dur = fmtDur(d)
		}
		c.text(x+1, y+1+i, fmt.Sprintf("%s%2d %s", mark, sub, name), rowSt, w-3-len(dur)-1)
		c.text(x+w-2-len(dur), y+1+i, dur, rowSt, len(dur))
	}
}

// scrollTop keeps sel inside a window of rows starting at top.
func scrollTop(top, sel, rows int) int {
	if rows <= 0 {
		return 0
	}
	if sel < top {
		top = sel
	}
	if sel >= top+rows {
		top = sel - rows + 1
	}
	return max(0, top)
}
