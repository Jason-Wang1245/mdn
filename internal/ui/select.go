package ui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// selectTo extends the selection from the cursor (or from the anchor of an
// existing selection) to wherever move puts the cursor: the line or document
// start or end.
//
// The textarea selects only by character, word or line, and keeps its
// selection anchor private. But its shift+arrow selection keeps the anchor
// once a selection exists and sets the head to the cursor, and the public
// cursor moves leave the selection alone. So: a shift+→/shift+← pair that
// goes nowhere fixes the anchor, move jumps the cursor, and a second pair
// pulls the head to it.
func (m *Model) selectTo(move func(*textarea.Model)) {
	m.syncSelectionHead()
	move(&m.editor)
	m.syncSelectionHead()
	if !m.editor.HasSelection() {
		// Don't leave an empty selection behind: the textarea would keep
		// its stale anchor for the next shift+arrow.
		m.editor.ClearSelection()
	}
}

// syncSelectionHead starts a keyboard selection if there is none and moves
// its head to the cursor, with two shift+arrow presses that cancel out.
func (m *Model) syncSelectionHead() {
	right := tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
	left := tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
	// At the very end of the buffer shift+→ can't move, so go left first.
	if m.editor.Line() == m.editor.LineCount()-1 && m.editor.Column() >= len(m.currentLine()) {
		right, left = left, right
	}
	m.editor, _ = m.editor.Update(right)
	m.editor, _ = m.editor.Update(left)
}

// selectionMove returns the cursor move for a shift+home/end-style
// selection key, if msg is one.
func (m Model) selectionMove(msg tea.KeyPressMsg) (func(*textarea.Model), bool) {
	switch {
	case key.Matches(msg, m.keys.SelectLineStart):
		return (*textarea.Model).CursorStart, true
	case key.Matches(msg, m.keys.SelectLineEnd):
		return (*textarea.Model).CursorEnd, true
	case key.Matches(msg, m.keys.SelectDocStart):
		return (*textarea.Model).MoveToBegin, true
	case key.Matches(msg, m.keys.SelectDocEnd):
		return (*textarea.Model).MoveToEnd, true
	}
	return nil, false
}
