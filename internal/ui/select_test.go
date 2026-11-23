package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func shiftKey(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: tea.ModShift | mod}
}

// moveTo puts the cursor at a logical (row, col).
func moveTo(m Model, row, col int) Model {
	m.editor.MoveToBegin()
	for range row {
		m.editor.CursorDown()
	}
	m.editor.SetCursorColumn(col)
	return m
}

func TestSelectToLineStartEnd(t *testing.T) {
	const text = "first line\nhello world\nlast"
	for _, c := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"shift+home", shiftKey(tea.KeyHome, 0), "hello"},
		{"shift+end", shiftKey(tea.KeyEnd, 0), " world"},
		{"cmd+shift+left", shiftKey(tea.KeyLeft, tea.ModSuper), "hello"},
		{"cmd+shift+right", shiftKey(tea.KeyRight, tea.ModSuper), " world"},
		{"ctrl+shift+home", shiftKey(tea.KeyHome, tea.ModCtrl), "first line\nhello"},
		{"ctrl+shift+end", shiftKey(tea.KeyEnd, tea.ModCtrl), " world\nlast"},
		{"cmd+shift+up", shiftKey(tea.KeyUp, tea.ModSuper), "first line\nhello"},
		{"cmd+shift+down", shiftKey(tea.KeyDown, tea.ModSuper), " world\nlast"},
	} {
		m, _ := newTestModel(t, text)
		m = send(moveTo(m, 1, 5), c.key)
		if got := m.editor.SelectedText(); got != c.want {
			t.Errorf("%s: selected %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSelectWholeLine(t *testing.T) {
	m, _ := newTestModel(t, "one\ntwo three\nfour")
	m = moveTo(m, 1, 4)
	m = send(m, tea.KeyPressMsg{Code: tea.KeyHome}, shiftKey(tea.KeyEnd, 0))
	if got := m.editor.SelectedText(); got != "two three" {
		t.Fatalf("home, shift+end selected %q", got)
	}
	// Extending back past the anchor flips the selection to the other side.
	m = send(m, shiftKey(tea.KeyHome, 0), shiftKey(tea.KeyHome, tea.ModCtrl))
	if got := m.editor.SelectedText(); got != "one\n" {
		t.Fatalf("then ctrl+shift+home selected %q", got)
	}
	// Typing replaces the selection.
	m = send(m, tea.KeyPressMsg{Code: 'X', Text: "X"})
	if got := m.editor.Value(); got != "Xtwo three\nfour" {
		t.Fatalf("typing over selection gave %q", got)
	}
}

// Selections at the very ends of the buffer, where one of the shift+arrow
// presses can't move.
func TestSelectAtBufferEdges(t *testing.T) {
	m, _ := newTestModel(t, "ab\ncd")
	m = send(moveTo(m, 1, 2), shiftKey(tea.KeyHome, 0))
	if got := m.editor.SelectedText(); got != "cd" {
		t.Fatalf("shift+home at buffer end selected %q", got)
	}

	m, _ = newTestModel(t, "ab\ncd")
	m = send(moveTo(m, 0, 0), shiftKey(tea.KeyEnd, tea.ModCtrl))
	if got := m.editor.SelectedText(); got != "ab\ncd" {
		t.Fatalf("ctrl+shift+end from buffer start selected %q", got)
	}

	// Nothing to select: no selection, and a later shift+→ starts afresh
	// at the cursor.
	m, _ = newTestModel(t, "ab\ncd")
	m = send(moveTo(m, 1, 2), shiftKey(tea.KeyEnd, 0))
	if m.editor.HasSelection() {
		t.Fatalf("shift+end at line end selected %q", m.editor.SelectedText())
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyHome}, shiftKey(tea.KeyRight, 0))
	if got := m.editor.SelectedText(); got != "c" {
		t.Fatalf("shift+→ afterwards selected %q, want %q", got, "c")
	}

	m, _ = newTestModel(t, "")
	m = send(m, shiftKey(tea.KeyEnd, 0), shiftKey(tea.KeyHome, tea.ModCtrl))
	if m.editor.HasSelection() {
		t.Fatal("empty buffer should have nothing selected")
	}
}

func TestSelectExtendsMouseSelection(t *testing.T) {
	m, _ := newTestModel(t, "hello world")
	m = send(m, click(gutter+2, 1), release(gutter+4, 1), shiftKey(tea.KeyEnd, 0))
	if got := m.editor.SelectedText(); got != "llo world" {
		t.Fatalf("shift+end after a drag selected %q", got)
	}
}
