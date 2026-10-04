package main

import (
	"bytes"
	"os"
	"strconv"

	"golang.org/x/term"
)

type key int

const (
	keyNone key = iota
	keyRune
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPgUp
	keyPgDn
	keyHome
	keyEnd
	keyEnter
	keyTab
	keyQuit
	keyClick
	keyWheelUp
	keyWheelDown
)

type event struct {
	key  key
	r    rune
	x, y int // mouse position, 0-based
}

// terminal puts the tty into raw mode on the alternate screen with mouse
// reporting and delivers decoded input events.
type terminal struct {
	fd     int
	state  *term.State
	color  bool
	events chan event
}

func openTerminal() (*terminal, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	t := &terminal{
		fd:     fd,
		state:  state,
		color:  os.Getenv("NO_COLOR") == "",
		events: make(chan event, 32),
	}
	// Alternate screen, hidden cursor, SGR mouse reporting.
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h\x1b[2J")
	go t.readInput()
	return t, nil
}

func (t *terminal) restore() {
	os.Stdout.WriteString("\x1b[?1006l\x1b[?1000l\x1b[0m\x1b[?25h\x1b[?1049l")
	term.Restore(t.fd, t.state)
}

func (t *terminal) clear() { os.Stdout.WriteString("\x1b[2J") }

func (t *terminal) size() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80, 24
	}
	return w, h
}

func (t *terminal) readInput() {
	defer close(t.events)
	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for _, ev := range parseInput(buf[:n]) {
			t.events <- ev
		}
	}
}

// parseInput decodes keys, CSI sequences and SGR mouse reports.
func parseInput(b []byte) []event {
	var evs []event
	for len(b) > 0 {
		ev, n := parseOne(b)
		b = b[n:]
		if ev.key != keyNone {
			evs = append(evs, ev)
		}
	}
	return evs
}

func parseOne(b []byte) (event, int) {
	c := b[0]
	switch {
	case c == 0x03 || c == 0x04: // Ctrl-C, Ctrl-D
		return event{key: keyQuit}, 1
	case c == '\r' || c == '\n':
		return event{key: keyEnter}, 1
	case c == '\t':
		return event{key: keyTab}, 1
	case c == 0x1b && len(b) == 1:
		return event{key: keyQuit}, 1 // a lone Escape
	case c == 0x1b && (b[1] == '[' || b[1] == 'O'):
		return parseCSI(b)
	case c == 0x1b:
		return event{}, 1
	case c < 0x20 || c == 0x7f:
		return event{}, 1
	}
	r := []rune(string(b[:utf8Len(c)]))
	return event{key: keyRune, r: r[0]}, max(1, utf8Len(c))
}

func utf8Len(c byte) int {
	switch {
	case c >= 0xf0:
		return 4
	case c >= 0xe0:
		return 3
	case c >= 0xc0:
		return 2
	}
	return 1
}

func parseCSI(b []byte) (event, int) {
	// Find the final byte (0x40..0x7e) after ESC [ or ESC O.
	end := 2
	for end < len(b) && (b[end] < 0x40 || b[end] > 0x7e) {
		end++
	}
	if end >= len(b) {
		return event{}, len(b)
	}
	params, final := b[2:end], b[end]
	n := end + 1
	if len(params) > 0 && params[0] == '<' { // SGR mouse: ESC [ < btn ; x ; y M/m
		f := bytes.Split(params[1:], []byte{';'})
		if len(f) != 3 || final != 'M' {
			return event{}, n
		}
		btn, _ := strconv.Atoi(string(f[0]))
		x, _ := strconv.Atoi(string(f[1]))
		y, _ := strconv.Atoi(string(f[2]))
		switch btn {
		case 0:
			return event{key: keyClick, x: x - 1, y: y - 1}, n
		case 64:
			return event{key: keyWheelUp}, n
		case 65:
			return event{key: keyWheelDown}, n
		}
		return event{}, n
	}
	switch final {
	case 'A':
		return event{key: keyUp}, n
	case 'B':
		return event{key: keyDown}, n
	case 'C':
		return event{key: keyRight}, n
	case 'D':
		return event{key: keyLeft}, n
	case 'H':
		return event{key: keyHome}, n
	case 'F':
		return event{key: keyEnd}, n
	case 'Z':
		return event{key: keyTab}, n // Shift-Tab
	case '~':
		switch string(params) {
		case "5":
			return event{key: keyPgUp}, n
		case "6":
			return event{key: keyPgDn}, n
		case "1", "7":
			return event{key: keyHome}, n
		case "4", "8":
			return event{key: keyEnd}, n
		}
	}
	return event{}, n
}
