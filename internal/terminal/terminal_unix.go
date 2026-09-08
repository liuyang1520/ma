//go:build darwin || linux

package terminal

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/liuyang1520/ma/internal/kitty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type Size struct{ Columns, Rows, PixelWidth, PixelHeight int }

type Terminal struct {
	file                  *os.File
	old                   *term.State
	Out                   *bufio.Writer
	Events                <-chan Event
	done                  chan struct{}
	cellWidth, cellHeight int
	active                bool
}

func Open(force bool) (*Terminal, error) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil, fmt.Errorf("stdout is not a terminal; use --export output.png for a PNG")
	}
	if os.Getenv("TMUX") != "" && !force {
		return nil, fmt.Errorf("tmux graphics passthrough is not supported yet; run ma outside tmux")
	}
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	old, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		f.Close()
		return nil, err
	}
	t := &Terminal{file: f, old: old, Out: bufio.NewWriterSize(os.Stdout, 64<<10), done: make(chan struct{})}
	t.Events = ReadEvents(f, t.done)
	_ = kitty.Query(t.Out)
	fmt.Fprint(t.Out, "\x1b[16t\x1b[14t")
	_ = t.Out.Flush()
	supported := false
	timer := time.NewTimer(800 * time.Millisecond)
probe:
	for {
		select {
		case e, ok := <-t.Events:
			if !ok || e.Key == "quit" {
				timer.Stop()
				t.Close()
				return nil, fmt.Errorf("terminal input interrupted")
			}
			if e.Key == "graphics" && strings.Contains(e.Text, fmt.Sprintf("i=%d", kitty.QueryID)) {
				supported = strings.HasSuffix(e.Text, ";OK")
			}
			if e.Key == "size6" {
				t.cellHeight = e.A
				t.cellWidth = e.B
			}
			if supported && t.cellHeight > 0 {
				break probe
			}
		case <-timer.C:
			break probe
		}
	}
	timer.Stop()
	if !supported && !force {
		t.Close()
		return nil, fmt.Errorf("terminal did not acknowledge Kitty graphics; use Ghostty/Kitty, or --force-graphics to skip detection")
	}
	t.active = true
	fmt.Fprint(t.Out, "\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h\x1b[2J")
	if err = t.Out.Flush(); err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (t *Terminal) Size() Size {
	s := Size{Columns: 80, Rows: 24}
	if w, err := unix.IoctlGetWinsize(int(t.file.Fd()), unix.TIOCGWINSZ); err == nil {
		s.Columns = max(1, int(w.Col))
		s.Rows = max(2, int(w.Row))
		s.PixelWidth = int(w.Xpixel)
		s.PixelHeight = int(w.Ypixel)
	}
	if s.PixelWidth == 0 {
		s.PixelWidth = s.Columns * max(t.cellWidth, 10)
	}
	if s.PixelHeight == 0 {
		s.PixelHeight = s.Rows * max(t.cellHeight, 20)
	}
	return s
}

func (t *Terminal) Status(text string, light bool) error {
	s := t.Size()
	// Only plain printable text reaches the status line, including filenames
	// and search queries supplied by the user.
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		if r > 126 {
			return '?'
		}
		return r
	}, text)
	if len(text) > s.Columns {
		text = text[:s.Columns]
	}
	colors := "\x1b[48;2;21;27;35m\x1b[38;2;145;152;161m"
	if light {
		colors = "\x1b[48;2;246;248;250m\x1b[38;2;89;99;110m"
	}
	_, err := fmt.Fprintf(t.Out, "\x1b[%d;1H%s%s\x1b[K\x1b[0m", s.Rows, colors, text)
	if err != nil {
		return err
	}
	return t.Out.Flush()
}

func (t *Terminal) Close() {
	close(t.done)
	if t.active {
		_ = kitty.Delete(t.Out)
		fmt.Fprint(t.Out, "\x1b[?1000l\x1b[?1006l\x1b[0m\x1b[?25h\x1b[?1049l")
	}
	_ = t.Out.Flush()
	_ = term.Restore(int(t.file.Fd()), t.old)
	_ = t.file.Close()
}
