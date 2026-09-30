package terminal

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Event struct {
	Key, Text string
	A, B      int
	Mouse     *Mouse
}

type Mouse struct {
	X, Y, Button, Modifiers int
	Released, Motion        bool
}

// Decoder tolerates escape sequences and UTF-8 split across arbitrary reads.
type Decoder struct{ pending []byte }

func (d *Decoder) Feed(data []byte) []Event {
	d.pending = append(d.pending, data...)
	var events []Event
	for len(d.pending) > 0 {
		p := d.pending
		if p[0] != 27 {
			if !utf8.FullRune(p) {
				break
			}
			r, size := utf8.DecodeRune(p)
			d.pending = p[size:]
			key := string(r)
			switch r {
			case 3:
				key = "quit"
			case 4:
				key = "halfdown"
			case 21:
				key = "halfup"
			case 6:
				key = "pagedown"
			case 2:
				key = "pageup"
			case 12:
				key = "redraw"
			case 13, 10:
				key = "enter"
			case 127, 8:
				key = "backspace"
			case 9:
				key = "tab"
			}
			events = append(events, Event{Key: key, Text: string(r)})
			continue
		}
		if len(p) < 2 {
			break
		}
		if p[1] == '_' || p[1] == ']' || p[1] == 'P' {
			end := bytes.Index(p, []byte{27, '\\'})
			if end < 0 {
				if len(p) > 8192 {
					d.pending = nil
				}
				break
			}
			body := string(p[2:end])
			d.pending = p[end+2:]
			if strings.HasPrefix(body, "G") {
				events = append(events, Event{Key: "graphics", Text: body})
			}
			continue
		}
		if p[1] == '[' || p[1] == 'O' {
			end := 2
			for end < len(p) && (p[end] < 0x40 || p[end] > 0x7e) {
				end++
			}
			if end >= len(p) {
				break
			}
			seq := string(p[2 : end+1])
			d.pending = p[end+1:]
			key := map[string]string{"A": "up", "B": "down", "C": "right", "D": "left", "H": "home", "F": "end", "1~": "home", "4~": "end", "5~": "pageup", "6~": "pagedown", "7~": "home", "8~": "end", "Z": "backtab", "1;3D": "back", "1;3C": "forward"}[seq]
			if key != "" {
				events = append(events, Event{Key: key})
				continue
			}
			if strings.HasSuffix(seq, "t") {
				var kind, a, b int
				if _, err := fmt.Sscanf(seq, "%d;%d;%dt", &kind, &a, &b); err == nil {
					events = append(events, Event{Key: fmt.Sprintf("size%d", kind), A: a, B: b})
				}
			}
			if strings.HasPrefix(seq, "?1016;") && strings.HasSuffix(seq, "$y") {
				if state, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(seq, "?1016;"), "$y")); err == nil {
					events = append(events, Event{Key: "mousemode", A: state})
				}
			}
			if strings.HasPrefix(seq, "<") && (strings.HasSuffix(seq, "M") || strings.HasSuffix(seq, "m")) {
				if event, ok := mouseEvent(seq); ok {
					events = append(events, event)
				}
			}
			continue
		}
		d.pending = p[1:]
		events = append(events, Event{Key: "escape"})
	}
	return events
}

func mouseEvent(seq string) (Event, bool) {
	parts := strings.Split(seq[1:len(seq)-1], ";")
	if len(parts) != 3 {
		return Event{}, false
	}
	button, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || button < 0 || button > 255 || x < 1 || y < 1 {
		return Event{}, false
	}
	released := seq[len(seq)-1] == 'm'
	if button&64 != 0 {
		if !released && button&3 <= 1 {
			key := "wheelup"
			if button&3 == 1 {
				key = "wheeldown"
			}
			return Event{Key: key}, true
		}
		return Event{}, false
	}
	which := button & 3
	if button&128 != 0 {
		which += 4
	}
	return Event{Key: "mouse", Mouse: &Mouse{X: x, Y: y, Button: which, Modifiers: button & 28, Released: released, Motion: button&32 != 0}}, true
}

func ReadEvents(r io.Reader, done <-chan struct{}) <-chan Event {
	out := make(chan Event, 128)
	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		for {
			buf := make([]byte, 4096)
			n, err := r.Read(buf)
			if n > 0 {
				select {
				case chunks <- buf[:n]:
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer close(out)
		var d Decoder
		timer := time.NewTicker(40 * time.Millisecond)
		defer timer.Stop()
		send := func(e Event) bool {
			select {
			case out <- e:
				return true
			case <-done:
				return false
			}
		}
		for {
			select {
			case chunk, ok := <-chunks:
				if !ok {
					return
				}
				for _, e := range d.Feed(chunk) {
					if !send(e) {
						return
					}
				}
			case <-timer.C:
				if len(d.pending) == 1 && d.pending[0] == 27 {
					d.pending = nil
					if !send(Event{Key: "escape"}) {
						return
					}
				}
			case <-done:
				return
			}
		}
	}()
	return out
}
