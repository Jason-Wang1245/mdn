package doc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileIsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.md")
	d, content, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if content != "" || d.Dirty("") {
		t.Fatalf("want empty clean doc, got %q dirty=%v", content, d.Dirty(""))
	}
	if !d.Dirty("x") {
		t.Fatal("expected dirty after change")
	}
}

func TestSaveRoundTripKeepsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.md")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, content, err := Load(p)
	if err != nil || content != "old" {
		t.Fatalf("load: %q %v", content, err)
	}
	if err := d.Save("# new\n"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "# new\n" {
		t.Fatalf("got %q", b)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", fi.Mode().Perm())
	}
	if d.Dirty("# new\n") {
		t.Fatal("should be clean after save")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestEmptyPathFails(t *testing.T) {
	if _, _, err := Load(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "notes.md")
	d, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Save("hi"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "hi" {
		t.Fatalf("got %q", b)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv(EnvFile, "/x/y.md")
	if p, _ := DefaultPath(); p != "/x/y.md" {
		t.Fatal(p)
	}
	t.Setenv(EnvFile, "")
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "notes.md" || filepath.Base(filepath.Dir(p)) != "mdn" {
		t.Fatal(p)
	}
}
