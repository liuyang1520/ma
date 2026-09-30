package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentNavigationRebindsReloadAndWatch(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.md"), filepath.Join(dir, "second.md")
	if err := os.WriteFile(first, []byte("# First\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("# Second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := openDocument(second, true)
	if err != nil || doc.Path != second || doc.BaseDir != dir || doc.Name != "second.md" {
		t.Fatal(doc, err)
	}
	updated := []byte("# Updated destination\n")
	if err := os.WriteFile(second, updated, 0600); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() ([]byte, error){doc.Reload, doc.Watch} {
		got, err := read()
		if err != nil || !bytes.Equal(got, updated) {
			t.Fatal("destination callback read the wrong file", string(got), err)
		}
	}
	if _, err := openDocument(dir, false); err == nil {
		t.Fatal("directory accepted as Markdown")
	}
	if _, err := openDocument(filepath.Join(dir, "missing.md"), false); err == nil {
		t.Fatal("missing destination accepted")
	}
}
