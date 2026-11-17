// Package doc handles loading and saving a single Markdown document.
package doc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Doc is a single document on disk.
type Doc struct {
	Path  string // empty for an untitled buffer
	saved string // content as of the last load/save
}

// Load reads path. A missing file is not an error: it yields an empty
// document that will be created on first save.
func Load(path string) (*Doc, string, error) {
	d := &Doc{Path: path}
	if path == "" {
		return d, "", nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return d, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	d.saved = string(b)
	return d, d.saved, nil
}

// Name is the display name of the document.
func (d *Doc) Name() string {
	if d.Path == "" {
		return "untitled.md"
	}
	return filepath.Base(d.Path)
}

// Dirty reports whether content differs from what is on disk.
func (d *Doc) Dirty(content string) bool { return content != d.saved }

// Save writes content to d.Path atomically (temp file + rename), keeping the
// existing file's permissions.
func (d *Doc) Save(content string) error {
	if d.Path == "" {
		return errors.New("no file name")
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(d.Path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(d.Path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(d.Path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpName, d.Path); err != nil {
		return err
	}
	d.saved = content
	return nil
}
