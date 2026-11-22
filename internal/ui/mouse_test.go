package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// gutter is the width of the line-number column (MaxHeight 999 → "999" + 2).
const gutter = 5

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func drag(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func release(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestMouseDragSelects(t *testing.T) {
	m, _ := newTestModel(t, "hello world\nsecond line\nthird")
	// Screen row 1 is the first editor line (row 0 is the title bar).
	m = send(m, click(gutter+6, 1), drag(gutter+8, 1), drag(gutter+6, 2), release(gutter+6, 2))
	if got, want := m.editor.SelectedText(), "world\nsecond"; got != want {
		t.Fatalf("selected %q, want %q", got, want)
	}
	if m.dragging {
		t.Fatal("drag should have ended on release")
	}
}

func TestMouseDragBackwards(t *testing.T) {
	m, _ := newTestModel(t, "hello world")
	m = send(m, click(gutter+5, 1), release(gutter+0, 1))
	if got := m.editor.SelectedText(); got != "hello" {
		t.Fatalf("selected %q, want %q", got, "hello")
	}
}

func TestMouseClickPlacesCursor(t *testing.T) {
	m, _ := newTestModel(t, "hello world\nsecond line")
	m = send(m, click(gutter+3, 2), release(gutter+3, 2))
	if m.editor.HasSelection() {
		t.Fatalf("a plain click should not select; got %q", m.editor.SelectedText())
	}
	if m.editor.Line() != 1 || m.editor.Column() != 3 {
		t.Fatalf("cursor at %d:%d, want 1:3", m.editor.Line(), m.editor.Column())
	}
}

func TestMouseSelectionCopyAndReplace(t *testing.T) {
	m, _ := newTestModel(t, "hello world")
	m = send(m, click(gutter+0, 1), release(gutter+5, 1))
	if _, cmd := m.Update(ctrl('c')); cmd == nil {
		t.Fatal("copy with a selection should return a clipboard command")
	}
	m = send(m, typeText("bye")...)
	if got := m.editor.Value(); got != "bye world" {
		t.Fatalf("typing over the selection: %q", got)
	}
}

func TestMouseCut(t *testing.T) {
	m, _ := newTestModel(t, "hello world")
	m = send(m, click(gutter+5, 1), release(gutter+11, 1))
	nm, cmd := m.Update(ctrl('x'))
	m = nm.(Model)
	if cmd == nil || m.editor.Value() != "hello" {
		t.Fatalf("cut: value %q, cmd nil=%v", m.editor.Value(), cmd == nil)
	}
}

func TestMouseDragScrollsPastBottom(t *testing.T) {
	var lines []string
	for i := range 100 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	m, _ := newTestModel(t, strings.Join(lines, "\n"))
	h := m.editor.Height()
	m = send(m, click(gutter, 1))
	// Each motion event below the pane extends the selection by one line,
	// rather than jumping to wherever the pointer is.
	for range 5 {
		m = send(m, drag(gutter, 50))
	}
	m = send(m, release(gutter, 50)) // the release extends it once more
	if got, want := m.editor.Line(), h+5; got != want {
		t.Fatalf("cursor on line %d, want %d", got, want)
	}
	if m.editor.ScrollYOffset() == 0 {
		t.Fatal("editor should have scrolled to follow the drag")
	}
	if !strings.HasPrefix(m.editor.SelectedText(), "line 0\n") {
		t.Fatalf("selection should start at the top: %q", m.editor.SelectedText())
	}
}

func TestMouseIgnoredOutsideEditor(t *testing.T) {
	m, _ := newTestModel(t, "hello world")

	// Read mode: no editor on screen.
	m = send(m, ctrl('r'))
	m = send(m, click(gutter, 1), release(gutter+5, 1))
	if m.editor.HasSelection() || m.dragging {
		t.Fatal("read mode should ignore clicks")
	}

	// Split mode: clicks in the preview pane don't start a selection.
	m = send(m, ctrl('r'), ctrl('\\'))
	edW, _ := m.paneWidths()
	m = send(m, click(edW+3, 1), drag(edW+8, 1), release(edW+8, 1))
	if m.editor.HasSelection() || m.dragging {
		t.Fatal("clicks in the preview should not select")
	}

	// …but a drag that starts in the editor may wander into it.
	m = send(m, click(gutter, 1), release(edW+8, 1))
	if got := m.editor.SelectedText(); got != "hello world" {
		t.Fatalf("selected %q", got)
	}

	// The title bar isn't part of the editor.
	m = send(m, tea.KeyPressMsg{Code: tea.KeyRight})
	m = send(m, click(gutter, 0), release(gutter+5, 0))
	if m.editor.HasSelection() {
		t.Fatal("title bar clicks should not select")
	}
}
