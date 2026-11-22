package ui

import tea "charm.land/bubbletea/v2"

// editorTop is the screen row of the editor's first line (below the title bar).
const editorTop = 1

// repositionMsg means nothing to the textarea, but sending it runs the
// scroll-to-cursor step at the end of its Update, which the selection methods
// skip. That keeps the cursor in view while a drag runs past the pane's edge.
type repositionMsg struct{}

// mouseSelectable reports whether clicks should go to the editor.
func (m Model) mouseSelectable() bool {
	return m.mode != modeRead && !m.showHelp && m.prompt == promptNone && m.cp.signIn == nil
}

// inEditor reports whether screen coordinates fall inside the editor pane.
func (m Model) inEditor(x, y int) bool {
	edW, _ := m.paneWidths()
	y -= editorTop
	return y >= 0 && y < m.editor.Height() && x >= 0 && x < edW
}

// editorCoords converts screen coordinates to textarea-relative ones. They
// are clamped to the pane, give or take one row above and below, so a drag
// past the top or bottom scrolls one line per motion event, not jumping to
// the line under the pointer.
func (m Model) editorCoords(x, y int) (int, int) {
	edW, _ := m.paneWidths()
	x = min(max(x, 0), edW-1)
	y = min(max(y-editorTop, -1), m.editor.Height())
	return x, y
}

// handleMouse selects text by dragging: a click places the cursor and starts
// a selection, motion extends it, and the release ends it (a plain click
// leaves no selection). Copy and cut then work on the highlighted text.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft || !m.mouseSelectable() || !m.inEditor(mouse.X, mouse.Y) {
			return m, nil
		}
		m.dragging = true
		m.slash = slashMenu{}
		m.editor.BeginSelection(m.editorCoords(mouse.X, mouse.Y))
	case tea.MouseMotionMsg:
		if !m.dragging {
			return m, nil
		}
		m.editor.ExtendSelection(m.editorCoords(mouse.X, mouse.Y))
	case tea.MouseReleaseMsg:
		if !m.dragging {
			return m, nil
		}
		m.dragging = false
		m.editor.ExtendSelection(m.editorCoords(mouse.X, mouse.Y))
		m.editor.EndSelection()
	default:
		return m, nil
	}
	m.editor, _ = m.editor.Update(repositionMsg{})
	// Moving the cursor invalidates suggestions.
	m.clearGhosts()
	m.cancelCompletion()
	if m.mode == modeSplit {
		m.syncScroll()
	}
	return m, nil
}

// copySelection copies the selected text to the system clipboard and, by
// OSC 52, to the terminal's, so copying also works over SSH.
func (m *Model) copySelection() tea.Cmd {
	if !m.editor.HasSelection() {
		return nil
	}
	return tea.Batch(m.editor.CopySelection(), tea.SetClipboard(m.editor.SelectedText()))
}
