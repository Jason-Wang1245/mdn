package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"mdn/internal/copilot"
)

// suggest injects a completion response for the current cursor position.
func suggest(m Model, texts ...string) Model {
	items := make([]copilot.InlineCompletionItem, len(texts))
	for i, s := range texts {
		items[i] = copilot.InlineCompletionItem{InsertText: s}
	}
	return send(m, cpCompletionMsg{version: m.cp.version, row: m.editor.Line(), col: m.editor.Column(), items: items})
}

func kp(code rune, mod ...tea.KeyMod) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: code}
	for _, md := range mod {
		k.Mod |= md
	}
	return k
}

var (
	tab = kp(tea.KeyTab)
	esc = kp(tea.KeyEscape)
)

func toEnd(m Model) Model { return send(m, kp(tea.KeyEnd, tea.ModCtrl)) }

func TestGhostShowAndAccept(t *testing.T) {
	m, _ := newTestModel(t, "# Shopping\n\n- ")
	m = toEnd(m)
	m = suggest(m, "apples\n- pears")
	out := plain(m)
	if !strings.Contains(out, "- apples") || !strings.Contains(out, "- pears") {
		t.Fatalf("ghost not rendered:\n%s", out)
	}
	if m.editor.Value() != "# Shopping\n\n- " {
		t.Fatalf("ghost leaked into buffer: %q", m.editor.Value())
	}

	m = send(m, tab)
	if got := m.editor.Value(); got != "# Shopping\n\n- apples\n- pears" {
		t.Fatalf("after accept: %q", got)
	}
	if _, ok := m.ghost(); ok {
		t.Fatal("ghost should be gone after accept")
	}
	if m.editor.Line() != 3 || m.editor.Column() != 7 {
		t.Fatalf("cursor at %d,%d", m.editor.Line(), m.editor.Column())
	}
}

func TestTabIndentsWithoutGhost(t *testing.T) {
	m, _ := newTestModel(t, "x")
	m = send(m, tab)
	if got := m.editor.Value(); got != "  x" {
		t.Fatalf("got %q", got)
	}
}

func TestTypeaheadKeepsGhost(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = toEnd(m)
	m = suggest(m, " world, how are you?")
	m = send(m, typeText(" wor")...)
	g, ok := m.ghost()
	if !ok || g.Text != "ld, how are you?" {
		t.Fatalf("ghost after typeahead: %+v %v", g, ok)
	}
	m = send(m, tab)
	if got := m.editor.Value(); got != "Hello world, how are you?" {
		t.Fatalf("got %q", got)
	}
}

func TestDivergingTypeClearsGhost(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = toEnd(m)
	m = suggest(m, " world")
	m = send(m, typeText(" x")...)
	if _, ok := m.ghost(); ok {
		t.Fatal("ghost should be cleared")
	}
}

func TestEscDismissesGhost(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = toEnd(m)
	m = suggest(m, " world")
	m = send(m, esc)
	if _, ok := m.ghost(); ok {
		t.Fatal("esc should dismiss")
	}
	if strings.Contains(plain(m), "world") {
		t.Fatal("ghost still rendered")
	}
}

func TestCursorMoveClearsGhost(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = toEnd(m)
	m = suggest(m, " world")
	m = send(m, kp(tea.KeyLeft))
	if _, ok := m.ghost(); ok {
		t.Fatal("ghost should be cleared on cursor move")
	}
	m = send(m, kp(tea.KeyRight))
	if _, ok := m.ghost(); ok {
		t.Fatal("ghost should not come back")
	}
}

func TestAcceptWord(t *testing.T) {
	m, _ := newTestModel(t, "The")
	m = toEnd(m)
	m = suggest(m, " quick brown fox")
	m = send(m, kp(tea.KeyRight, tea.ModCtrl))
	if got := m.editor.Value(); got != "The quick" {
		t.Fatalf("got %q", got)
	}
	g, ok := m.ghost()
	if !ok || g.Text != " brown fox" {
		t.Fatalf("remaining ghost: %+v %v", g, ok)
	}
	m = send(m, tab)
	if got := m.editor.Value(); got != "The quick brown fox" {
		t.Fatalf("got %q", got)
	}
}

func TestCycleSuggestions(t *testing.T) {
	m, _ := newTestModel(t, "Hi")
	m = toEnd(m)
	m = suggest(m, " there", " friend", " there") // duplicate is dropped
	if n := len(m.cp.ghosts); n != 2 {
		t.Fatalf("want 2 ghosts, got %d", n)
	}
	m = send(m, kp(']', tea.ModAlt))
	if g, _ := m.ghost(); g.Text != " friend" {
		t.Fatalf("after alt+]: %q", g.Text)
	}
	m = send(m, kp('[', tea.ModAlt))
	if g, _ := m.ghost(); g.Text != " there" {
		t.Fatalf("after alt+[: %q", g.Text)
	}
}

func TestStaleCompletionIgnored(t *testing.T) {
	m, _ := newTestModel(t, "Hi")
	m = toEnd(m)
	m = send(m, cpCompletionMsg{version: m.cp.version, row: 0, col: 0,
		items: []copilot.InlineCompletionItem{{InsertText: "nope"}}})
	if _, ok := m.ghost(); ok {
		t.Fatal("completion for another position should be ignored")
	}
}

func TestReplacesRestOfLine(t *testing.T) {
	m, _ := newTestModel(t, "call(x)")
	m = toEnd(m)
	m = send(m, kp(tea.KeyLeft)) // cursor before ")"
	m = suggest(m, ", y)")       // ends with the existing ")"
	if g, ok := m.ghost(); !ok || g.DeleteAfter != 1 {
		t.Fatalf("ghost: %+v %v", g, ok)
	}
	m = send(m, tab)
	if got := m.editor.Value(); got != "call(x, y)" {
		t.Fatalf("got %q", got)
	}
}

func TestSlashMenuHidesGhost(t *testing.T) {
	m, _ := newTestModel(t, "")
	m = send(m, typeText("/")...)
	if !m.slash.active {
		t.Fatal("slash menu should be open")
	}
	m = suggest(m, "h1")
	if _, ok := m.ghost(); ok {
		t.Fatal("no ghost while slash menu is open")
	}
}

func TestGhostInSplitMode(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = send(m, ctrl('\\'))
	m = toEnd(m)
	m = suggest(m, " split world")
	out := plain(m)
	if !strings.Contains(out, "Hello split world") {
		t.Fatalf("ghost not in editor pane:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := len([]rune(line)); w > 80 {
			t.Fatalf("line %d is %d wide", i, w)
		}
	}
}

func TestReadModeClearsGhost(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = toEnd(m)
	m = suggest(m, " world")
	m = send(m, ctrl('e'))
	m = send(m, ctrl('e'))
	if _, ok := m.ghost(); ok {
		t.Fatal("ghost should not survive read mode")
	}
}

func TestCursorPlacedBelowTitleBar(t *testing.T) {
	m, _ := newTestModel(t, "Hello")
	m = toEnd(m)
	c := m.View().Cursor
	if c == nil {
		t.Fatal("expected a terminal cursor in edit mode")
	}
	if c.Y != 1 {
		t.Fatalf("cursor row %d, want 1", c.Y)
	}
	m = send(m, ctrl('e'))
	if m.View().Cursor != nil {
		t.Fatal("no cursor in read mode")
	}
}

func TestStatusIndicator(t *testing.T) {
	m, _ := newTestModel(t, "")
	if strings.Contains(plain(m), "Copilot") {
		t.Fatal("indicator should be hidden when Copilot is disabled")
	}
	m.opts.StartCopilot = func(context.Context, copilot.Options) (*copilot.Client, error) {
		return nil, errors.New("unused")
	}
	if !strings.Contains(plain(m), "Copilot …") {
		t.Fatalf("expected starting indicator:\n%s", plain(m))
	}
	m = send(m, cpErrMsg{errors.New("copilot-language-server not found")})
	if !strings.Contains(plain(m), "Copilot ✗") {
		t.Fatalf("expected error indicator:\n%s", plain(m))
	}
	m = send(m, typeText("x")...)
	m = send(m, kp(' ', tea.ModCtrl))
	if !strings.Contains(plain(m), "Copilot unavailable") {
		t.Fatalf("ctrl+space should explain why:\n%s", plain(m))
	}
}

// The real server returns items that replace the line from column 0.
func TestRangedItemFromServer(t *testing.T) {
	m, _ := newTestModel(t, "# Making tea\n\n1. Boil the water.\n2. ")
	m = toEnd(m)
	item := copilot.InlineCompletionItem{
		InsertText: "2. Place the tea bag in a cup.",
		Range:      &copilot.Range{Start: copilot.Position{Line: 3}, End: copilot.Position{Line: 3, Character: 3}},
	}
	m = send(m, cpCompletionMsg{version: m.cp.version, row: 3, col: 3, items: []copilot.InlineCompletionItem{item}})
	if g, ok := m.ghost(); !ok || g.Text != "Place the tea bag in a cup." {
		t.Fatalf("ghost: %+v %v", g, ok)
	}
	if !strings.Contains(plain(m), "2. Place the tea bag in a cup.") {
		t.Fatalf("not rendered:\n%s", plain(m))
	}
	m = send(m, tab)
	if got := m.editor.Value(); got != "# Making tea\n\n1. Boil the water.\n2. Place the tea bag in a cup." {
		t.Fatalf("got %q", got)
	}
}
