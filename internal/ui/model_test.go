package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func newTestModel(t *testing.T, content string) (Model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "note.md")
	if content != "" {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	return send(m, tea.WindowSizeMsg{Width: 80, Height: 24}), p
}

func send(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func typeText(s string) []tea.Msg {
	var msgs []tea.Msg
	for _, r := range s {
		if r == '\n' {
			msgs = append(msgs, tea.KeyPressMsg{Code: tea.KeyEnter})
			continue
		}
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return msgs
}

func plain(m Model) string { return ansi.Strip(m.View().Content) }

func TestModeSwitching(t *testing.T) {
	m, _ := newTestModel(t, "# Hello World\n\nSome **bold** text.\n")
	if m.mode != modeEdit || !strings.Contains(plain(m), "# Hello World") {
		t.Fatalf("expected raw markdown in edit mode:\n%s", plain(m))
	}

	m = send(m, ctrl('e'))
	if m.mode != modeRead {
		t.Fatalf("ctrl+e: mode=%v", m.mode)
	}
	out := plain(m)
	if strings.Contains(out, "**bold**") || !strings.Contains(out, "bold") {
		t.Fatalf("expected rendered markdown in read mode:\n%s", out)
	}

	m = send(m, ctrl('\\'))
	if m.mode != modeSplit {
		t.Fatalf("ctrl+\\: mode=%v", m.mode)
	}
	out = plain(m)
	if !strings.Contains(out, "**bold**") || !strings.Contains(out, "│") {
		t.Fatalf("split view should show source and divider:\n%s", out)
	}
	// Leaving split returns to the last solo mode (read).
	m = send(m, ctrl('\\'))
	if m.mode != modeRead {
		t.Fatalf("leaving split: mode=%v", m.mode)
	}
}

func TestViewFitsWindow(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {20, 5}, {3, 2}, {200, 60}} {
		m, _ := newTestModel(t, "# Title\n\n- a\n- b\n\n```go\nfunc main() {}\n```\n")
		m = send(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for _, md := range []tea.Msg{nil, ctrl('e'), ctrl('\\')} {
			if md != nil {
				m = send(m, md)
			}
			content := m.View().Content
			if h := lipgloss.Height(content); h > sz[1] {
				t.Errorf("%v mode %v: height %d > %d", sz, m.mode, h, sz[1])
			}
			if w := lipgloss.Width(content); w > sz[0] {
				t.Errorf("%v mode %v: width %d > %d", sz, m.mode, w, sz[0])
			}
		}
	}
}

func TestListContinuation(t *testing.T) {
	cases := []struct{ typed, want string }{
		{"- one\ntwo", "- one\n- two"},
		{"  * nested\nmore", "  * nested\n  * more"},
		{"1. first\nsecond", "1. first\n2. second"},
		{"9) a\nb", "9) a\n10) b"},
		{"- [x] done\ntodo", "- [x] done\n- [ ] todo"},
		// Enter on an empty item ends the list.
		{"- one\n\nafter", "- one\nafter"},
		{"1. one\n\nafter", "1. one\nafter"},
		{"plain\ntext", "plain\ntext"},
	}
	for _, c := range cases {
		m, _ := newTestModel(t, "")
		m = send(m, typeText(c.typed)...)
		if got := m.editor.Value(); got != c.want {
			t.Errorf("typed %q:\n got %q\nwant %q", c.typed, got, c.want)
		}
	}
}

func TestSaveAndDirtyAndQuitGuard(t *testing.T) {
	m, p := newTestModel(t, "hi")
	m = send(m, typeText("!")...)
	if !strings.Contains(plain(m), "●") {
		t.Fatal("expected dirty marker")
	}
	// Quit with unsaved changes asks first.
	nm, cmd := m.Update(ctrl('q'))
	m = nm.(Model)
	if m.prompt != promptConfirmQuit || cmd != nil {
		t.Fatal("expected quit confirmation prompt")
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.prompt != promptNone {
		t.Fatal("esc should cancel")
	}

	m = send(m, ctrl('s'))
	b, _ := os.ReadFile(p)
	if string(b) != m.editor.Value() || strings.Contains(plain(m), "●") {
		t.Fatalf("save failed: file=%q editor=%q", b, m.editor.Value())
	}
	if _, cmd := m.Update(ctrl('q')); cmd == nil {
		t.Fatal("clean quit should return tea.Quit")
	}
}

func TestUntitledSaveAs(t *testing.T) {
	m, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = send(m, typeText("# Draft")...)
	m = send(m, ctrl('s'))
	if m.prompt != promptSaveAs {
		t.Fatal("expected save-as prompt")
	}
	target := filepath.Join(t.TempDir(), "draft")
	m = send(m, tea.PasteMsg{Content: target})
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	b, err := os.ReadFile(target + ".md")
	if err != nil || string(b) != "# Draft" {
		t.Fatalf("got %q, %v", b, err)
	}
	if m.doc.Name() != "draft.md" {
		t.Fatal(m.doc.Name())
	}
}

func TestSplitPreviewUpdatesAfterDebounce(t *testing.T) {
	m, _ := newTestModel(t, "")
	m = send(m, ctrl('\\'))
	m = send(m, typeText("# Fresh heading")...)
	m = send(m, renderTickMsg{seq: m.renderSeq - 1}) // stale: ignored
	if m.renderedText == m.editor.Value() {
		t.Fatal("stale tick should not render")
	}
	m = send(m, renderTickMsg{seq: m.renderSeq})
	if m.renderedText != m.editor.Value() {
		t.Fatal("current tick should render")
	}
	if n := strings.Count(plain(m), "Fresh heading"); n < 2 {
		t.Fatalf("expected heading in editor and preview, found %d", n)
	}
}

func cmdKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModSuper} }

func TestCmdBindings(t *testing.T) {
	m, p := newTestModel(t, "# hi")
	m = send(m, cmdKey('e'))
	if m.mode != modeRead {
		t.Fatalf("cmd+e: mode=%v", m.mode)
	}
	m = send(m, cmdKey('\\'))
	if m.mode != modeSplit {
		t.Fatalf("cmd+\\: mode=%v", m.mode)
	}
	m = send(m, cmdKey('a'), tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.editor.Value() != "x" {
		t.Fatalf("cmd+a then typing should replace all, got %q", m.editor.Value())
	}
	m = send(m, cmdKey('s'))
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("cmd+s did not save: %q", b)
	}
	if _, c := m.Update(cmdKey('q')); c == nil {
		t.Fatal("cmd+q should quit")
	}
	if !strings.Contains(plain(m), "cmd+g help") {
		t.Fatal("status bar should advertise cmd keys")
	}
}
