// Package fsutil holds the one file-system idiom every store shares.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to path whole or not at all: it goes to a temporary
// file beside path and is renamed over it, so a crash mid-write leaves the
// old file, not half of the new one. mode is the file's permission bits,
// which the temporary file does not otherwise carry.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
