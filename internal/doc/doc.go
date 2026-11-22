// Package doc handles loading and saving a single Markdown document.
package doc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// EnvFile overrides the location of the global document.
const EnvFile = "MDN_FILE"

// DefaultPath is the global document mdn edits when no file is given:
// $MDN_FILE if set, otherwise notes.md in mdn's directory under the user
// config dir (~/Library/Application Support/mdn on macOS, ~/.config/mdn on
// Linux, %AppData%\mdn on Windows).
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvFile); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mdn", "notes.md"), nil
}

// Doc is a single document on disk.
type Doc struct {
	Path  string
	saved string // content as of the last load/save
}

// Load reads path. A missing file is not an error: it yields an empty
// document that will be created on first save.
func Load(path string) (*Doc, string, error) {
	if path == "" {
		return nil, "", errors.New("no file name")
	}
	d := &Doc{Path: path}
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
func (d *Doc) Name() string { return filepath.Base(d.Path) }

// Dirty reports whether content differs from what is on disk.
func (d *Doc) Dirty(content string) bool { return content != d.saved }

// Save writes content to d.Path atomically (temp file + rename), keeping the
// existing file's permissions. Missing parent directories are created.
func (d *Doc) Save(content string) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(d.Path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(d.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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
