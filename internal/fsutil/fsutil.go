// Package fsutil holds the file-system idioms the stores share: the atomic
// write, and the write through a link.
package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
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

// WriteThrough writes data to the file path names, whole or not at all, as
// WriteFile does, and returns the path it wrote. A link is written through
// and never replaced: path, or a directory on the way to it, may be a link
// into a dotfiles repository, and the write lands in the file the link points
// at, so the link stays. The file keeps its permission bits; a new one gets
// 0644, and its directory is made.
func WriteThrough(path string, data []byte) (string, error) {
	real, err := Resolve(path)
	if err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(real); err == nil {
		mode = info.Mode().Perm()
	}
	return real, WriteFile(real, data, mode)
}

// Resolve is the file path names, every link on the way followed: the file
// itself when it is there, else the file of that name in its directory,
// which is made when it is missing. A link that points at nothing is an
// error, because writing beside it would replace the link with a file.
func Resolve(path string) (string, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("%s is a link to nothing: %w", path, err)
		}
		return real, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(real, filepath.Base(path)), nil
}
