package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/liuyang1520/ma/internal/pager"
	"github.com/liuyang1520/ma/internal/render"
	"golang.org/x/term"
)

const version = "0.2.2"
const maxSource = 16 << 20

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ma:", err)
		os.Exit(1)
	}
}

func readSource(path string) ([]byte, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxSource+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSource {
		return nil, fmt.Errorf("Markdown exceeds 16 MiB input limit")
	}
	return b, nil
}

func run() error {
	fs := flag.NewFlagSet("ma", flag.ContinueOnError)
	theme := fs.String("theme", "dark", "GitHub-style theme: dark or light")
	fontSize := fs.Int("font-size", 24, "body font size in logical pixels (10–32)")
	scale := fs.Float64("scale", 0, "device pixel scale; 0 matches terminal cell size")
	export := fs.String("export", "", "save a viewport PNG without opening the pager")
	width := fs.Int("width", 1000, "export viewport width in logical pixels")
	height := fs.Int("height", 800, "export viewport height in logical pixels")
	force := fs.Bool("force-graphics", false, "skip Kitty capability detection")
	showVersion := fs.Bool("version", false, "print version")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: ma [options] FILE.md\n       cat FILE.md | ma\n\nA native Markdown pager with real heading sizes over Kitty graphics.\nRequires a Kitty graphics terminal such as Ghostty. No browser needed.\n\nKeys: j/k, arrows, space/b, d/u, g/G, /, n/N, +/-, t, r, ?, q.\n\nOptions:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println("ma", version)
		return nil
	}
	if *theme != "dark" && *theme != "light" {
		return fmt.Errorf("--theme must be dark or light")
	}
	if *fontSize < 10 || *fontSize > 32 {
		return fmt.Errorf("--font-size must be between 10 and 32")
	}
	if math.IsNaN(*scale) || math.IsInf(*scale, 0) || *scale < 0 || *scale > 4 || (*scale > 0 && *scale < .5) {
		return fmt.Errorf("--scale must be 0 (auto) or between 0.5 and 4")
	}
	if *width < 100 || *height < 100 || *width > 4096 || *height > 4096 {
		return fmt.Errorf("export width/height must be between 100 and 4096")
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("open one Markdown file at a time")
	}
	path := "-"
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	} else if term.IsTerminal(int(os.Stdin.Fd())) {
		fs.Usage()
		return nil
	}
	if path == "-" && term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("pipe Markdown into ma, or provide a file")
	}
	source, err := readSource(path)
	if err != nil {
		return err
	}
	baseDir, name := "", "stdin"
	if path != "-" {
		abs, e := filepath.Abs(path)
		if e != nil {
			return e
		}
		baseDir = filepath.Dir(abs)
		name = filepath.Base(abs)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	if *export != "" {
		b, e := render.New(ctx)
		if e != nil {
			return e
		}
		defer b.Close()
		s := *scale
		if s == 0 {
			s = 1
		}
		if e = b.Resize(*width, *height, s); e != nil {
			return e
		}
		if e = b.Load(source, baseDir, *theme, *fontSize); e != nil {
			return e
		}
		png, _, e := b.Frame(0)
		if e != nil {
			return e
		}
		if e = os.WriteFile(*export, png, 0644); e != nil {
			return e
		}
		fmt.Fprintln(os.Stderr, "Saved", *export)
		return nil
	}
	opts := pager.Options{Name: name, BaseDir: baseDir, Theme: *theme, Source: source, FontSize: *fontSize, Scale: *scale, ForceGraphics: *force}
	if path != "-" {
		opts.Reload = func() ([]byte, error) { return readSource(path) }
	}
	return pager.Run(ctx, opts)
}
