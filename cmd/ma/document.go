package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liuyang1520/ma/internal/pager"
)

func openDocument(path string, watch bool) (pager.Document, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return pager.Document{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return pager.Document{}, documentError(abs, err)
	}
	if !info.Mode().IsRegular() {
		return pager.Document{}, fmt.Errorf("Markdown destination is not a regular file: %s", abs)
	}
	source, err := readSource(abs)
	if err != nil {
		return pager.Document{}, documentError(abs, err)
	}
	doc := pager.Document{Path: abs, Name: filepath.Base(abs), BaseDir: filepath.Dir(abs), Source: source}
	doc.Reload = func() ([]byte, error) { return readSource(abs) }
	if watch {
		doc.Watch = fileWatcher(abs, source)
	}
	return doc, nil
}

// Keep the reason visible on a narrow status line instead of leading with a
// long absolute path. The focused link already shows its full destination.
func documentError(path string, err error) error {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		err = pathError.Err
	}
	return fmt.Errorf("cannot open %s: %w", filepath.Base(path), err)
}
