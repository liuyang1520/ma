package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchChangesAndAtomicSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	initial := []byte("# Before\n")
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	watch := fileWatcher(path, initial)
	for range 2 {
		if source, err := watch(); err != nil || source != nil {
			t.Fatal("unchanged file triggered reload", source, err)
		}
	}
	updated := []byte("# Update\n") // Same size; timestamp must also be checked.
	if err := os.WriteFile(path, updated, 0600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if source, err := watch(); err != nil || !bytes.Equal(source, updated) {
		t.Fatal("missed same-size edit", source, err)
	}
	replacement := path + ".new"
	next := []byte("# Atomic\n")
	if err := os.WriteFile(replacement, next, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if source, err := watch(); err != nil || !bytes.Equal(source, next) {
		t.Fatal("missed atomic save with identical timestamp/size", source, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := watch(); err == nil {
		t.Fatal("missing file did not report an error")
	}
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if source, err := watch(); err != nil || source == nil || len(source) != 0 {
		t.Fatal("watch did not recover and reload an empty file", source, err)
	}
	if source, err := watch(); err != nil || source != nil {
		t.Fatal("empty file reloaded repeatedly", source, err)
	}
}
