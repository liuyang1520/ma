package main

import (
	"bytes"
	"os"
)

// Stat avoids repeatedly reading unchanged files. SameFile also catches editors
// that save by atomically replacing a file, even with matching size/timestamps.
func fileWatcher(path string, initial []byte) func() ([]byte, error) {
	var previous os.FileInfo
	source := bytes.Clone(initial)
	return func() ([]byte, error) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if previous != nil && os.SameFile(previous, info) && previous.Size() == info.Size() && previous.ModTime() == info.ModTime() {
			return nil, nil
		}
		next, err := readSource(path)
		if err != nil {
			return nil, err
		}
		previous = info
		if bytes.Equal(source, next) {
			return nil, nil
		}
		source = bytes.Clone(next)
		return next, nil
	}
}
