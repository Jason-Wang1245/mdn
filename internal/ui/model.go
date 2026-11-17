// Package ui implements the mdn terminal editor.
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mdn/internal/doc"
)

type mode int

const (
	modeEdit mode = iota
	modeRead
	modeSplit
)

func (m mode) String() string {
	return [...]string{"EDIT", "READ", "SPLIT"}[m]
}

type promptKind int

const (
	promptNone promptKind = iota
	promptSaveAs
	promptConfirmQuit
)

type clearStatusMsg struct{ seq int }

// Model is the root Bubble Tea model.
type Model struct {
	doc     *doc.Doc
	editor  textarea.Model
	preview viewport.Model
	help    help.Model
	input   textinput.Model
	keys    keyMap
	st      styles
	rend    renderer

	dark          bool
	mode          mode
	lastSoloMode  mode // mode to return to when leaving split
	width, height int
	showHelp      bool
	prompt        promptKind
	quitAfterSave bool

	status    string
	statusErr bool
	statusSeq int

	renderSeq    int
	renderedText string // the source the preview currently shows
	renderedW    int
	renderedDark bool

	slash slashMenu
	now   func() time.Time
}

// New creates the editor for path ("" for an untitled buffer).
func New(path string) (Model, error) {
	d, content, err := doc.Load(path)
	if err != nil {
		return Model{}, err
	}

	ed := textarea.New()
	ed.KeyMap = editorKeys()
	ed.Prompt = ""
	ed.ShowLineNumbers = true
	ed.CharLimit = 0
	// MaxHeight caps the visible rows (not the document length) and sizes
	// the line-number gutter; 999 gives a stable 3-digit gutter.
	ed.MaxHeight = 999
	ed.MaxWidth = 0
	ed.Placeholder = "Start writing…"
	ed.SetValue(content)
	ed.MoveToBegin()
	ed.Focus()

	in := textinput.New()
	in.Prompt = "Save as: "
	in.Placeholder = "note.md"

	m := Model{
		doc:     d,
		editor:  ed,
		preview: viewport.New(),
		help:    help.New(),
		input:   in,
		keys:    defaultKeys(),
		dark:    true,
		now:     time.Now,
	}
	m.applyTheme()
	return m, nil
}

func (m Model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m *Model) applyTheme() {
	m.st = newStyles(m.dark)
	m.editor.SetStyles(textarea.DefaultStyles(m.dark))
	m.help.Styles = help.DefaultStyles(m.dark)
	m.input.SetStyles(textinput.DefaultStyles(m.dark))
}

// ---------------------------------------------------------------- layout

func (m Model) bodyHeight() int { return max(m.height-2, 1) }

// paneWidths returns editor and preview widths for the current mode.
func (m Model) paneWidths() (ed, pv int) {
	switch m.mode {
	case modeSplit:
		ed = max((m.width-1)/2, 1)
		return ed, max(m.width-ed-1, 1)
	case modeRead:
		return m.width, m.width
	default:
		return m.width, m.width
	}
}

func (m *Model) resize() {
	edW, pvW := m.paneWidths()
	h := m.bodyHeight()
	m.editor.SetWidth(max(edW, 1))
	m.editor.SetHeight(h)
	m.preview.SetWidth(pvW)
	m.preview.SetHeight(h)
	m.input.SetWidth(max(m.width-len(m.input.Prompt)-4, 10))
	m.help.SetWidth(m.width)
	m.refreshPreview(true)
}

// refreshPreview re-renders the preview if it is visible and stale.
func (m *Model) refreshPreview(force bool) {
	if m.mode == modeEdit || m.width == 0 {
		return
	}
	_, w := m.paneWidths()
	src := m.editor.Value()
	if !force && src == m.renderedText && w == m.renderedW && m.dark == m.renderedDark {
		return
	}
	off := m.preview.YOffset()
	m.preview.SetContent(m.rend.render(src, w, m.dark))
	m.preview.SetYOffset(off)
	m.renderedText, m.renderedW, m.renderedDark = src, w, m.dark
	if m.mode == modeSplit {
		m.syncScroll()
	}
}

// syncScroll keeps the preview roughly aligned with the editor cursor.
func (m *Model) syncScroll() {
	lines := m.editor.LineCount()
	if lines <= 1 {
		m.preview.GotoTop()
		return
	}
	frac := float64(m.editor.Line()) / float64(lines-1)
	maxOff := max(m.preview.TotalLineCount()-m.preview.Height(), 0)
	m.preview.SetYOffset(int(frac * float64(maxOff)))
}

func (m *Model) setMode(md mode) {
	if md != modeSplit {
		m.lastSoloMode = md
	}
	if md == modeRead {
		m.slash = slashMenu{}
	}
	m.mode = md
	m.resize()
	if md == modeRead {
		m.syncScroll()
	}
}

// ---------------------------------------------------------------- status

func (m *Model) setStatus(s string, isErr bool) tea.Cmd {
	m.status, m.statusErr = s, isErr
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{seq} })
}

// ---------------------------------------------------------------- update

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.applyTheme()
		m.refreshPreview(true)
		return m, nil

	case clearStatusMsg:
		if msg.seq == m.statusSeq {
			m.status = ""
		}
		return m, nil

	case renderTickMsg:
		if msg.seq == m.renderSeq {
			m.refreshPreview(false)
		}
		return m, nil

	case tea.MouseWheelMsg:
		m.slash = slashMenu{}
		return m.handleWheel(msg)

	case tea.PasteMsg:
		m.slash = slashMenu{}
		if m.prompt == promptSaveAs {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.mode == modeRead {
			return m, nil
		}
		return m.updateEditor(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Anything else (cursor blink, clipboard results, …) goes to the
	// focused components.
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	cmds = append(cmds, cmd)
	if m.prompt == promptSaveAs {
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	edW, _ := m.paneWidths()
	toPreview := m.mode == modeRead || (m.mode == modeSplit && msg.X > edW)
	if toPreview {
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}
	for range 3 {
		switch msg.Button {
		case tea.MouseWheelUp:
			m.editor.CursorUp()
		case tea.MouseWheelDown:
			m.editor.CursorDown()
		}
	}
	if m.mode == modeSplit {
		m.syncScroll()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.prompt {
	case promptSaveAs:
		return m.handleSaveAsKey(msg)
	case promptConfirmQuit:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Save):
			m.prompt = promptNone
			m.quitAfterSave = true
			return m.save()
		default:
			m.prompt = promptNone
			return m, m.setStatus("Quit cancelled", false)
		}
	}

	if m.showHelp {
		// Any key dismisses help; quit still works.
		m.showHelp = false
		if !key.Matches(msg, m.keys.Quit) {
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.doc.Dirty(m.editor.Value()) {
			m.slash = slashMenu{}
			m.prompt = promptConfirmQuit
			return m, nil
		}
		return m, tea.Quit
	case key.Matches(msg, m.keys.Save):
		return m.save()
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.ToggleRead):
		next := modeRead
		if m.mode == modeRead {
			next = modeEdit
		}
		m.setMode(next)
		return m, nil
	case key.Matches(msg, m.keys.Split):
		if m.mode == modeSplit {
			m.setMode(m.lastSoloMode)
		} else {
			m.setMode(modeSplit)
		}
		return m, nil
	}

	if m.mode == modeRead {
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}

	if m.slash.active {
		switch msg.String() {
		case "up", "ctrl+p":
			if n := len(m.slash.matches); n > 0 {
				m.slash.sel = (m.slash.sel - 1 + n) % n
			}
			return m, nil
		case "down", "ctrl+n":
			if n := len(m.slash.matches); n > 0 {
				m.slash.sel = (m.slash.sel + 1) % n
			}
			return m, nil
		case "esc":
			m.slash = slashMenu{}
			return m, nil
		case "enter", "tab":
			if len(m.slash.matches) > 0 {
				return m, m.applySlash()
			}
			m.slash = slashMenu{} // no match: behave normally
		}
	}

	switch {
	case key.Matches(msg, m.keys.Copy):
		if m.editor.HasSelection() {
			return m, m.editor.CopySelection()
		}
		return m, nil
	case key.Matches(msg, m.keys.Cut):
		if m.editor.HasSelection() {
			m.slash = slashMenu{}
			cmd := m.editor.CopySelection()
			m.editor.DeleteSelection()
			return m, tea.Batch(cmd, m.contentChanged())
		}
		return m, nil
	case key.Matches(msg, m.keys.Indent):
		m.slash = slashMenu{}
		if m.editor.HasSelection() {
			m.editor.DeleteSelection()
		}
		m.editor.InsertString("  ")
		return m, m.contentChanged()
	case msg.String() == "enter":
		if m.continueList() {
			return m, m.contentChanged()
		}
	}
	return m.updateEditor(msg)
}

// updateEditor forwards msg to the textarea and reacts to content changes.
func (m Model) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	m.syncSlash(msg)
	if m.editor.Value() != before {
		return m, tea.Batch(cmd, m.contentChanged())
	}
	if m.mode == modeSplit {
		m.syncScroll()
	}
	return m, cmd
}

// ---------------------------------------------------------------- slash menu

// currentLine returns the cursor's logical line as runes.
func (m Model) currentLine() []rune {
	lines := strings.Split(m.editor.Value(), "\n")
	if r := m.editor.Line(); r >= 0 && r < len(lines) {
		return []rune(lines[r])
	}
	return nil
}

// syncSlash opens the menu when "/" is typed at a word boundary and keeps
// its query in step with the text after the "/".
func (m *Model) syncSlash(msg tea.Msg) {
	line := m.currentLine()
	col := m.editor.Column()
	if !m.slash.active {
		kp, ok := msg.(tea.KeyPressMsg)
		if !ok || kp.Text != "/" || col < 1 || col > len(line) || line[col-1] != '/' {
			return
		}
		if col >= 2 && line[col-2] != ' ' && line[col-2] != '\t' {
			return
		}
		m.slash = slashMenu{active: true, row: m.editor.Line(), startCol: col - 1, query: "\x00"}
	}

	s := &m.slash
	if m.editor.Line() != s.row || col <= s.startCol || col > len(line) || line[s.startCol] != '/' {
		*s = slashMenu{}
		return
	}
	q := string(line[s.startCol+1 : col])
	if strings.ContainsAny(q, " \t/") {
		*s = slashMenu{}
		return
	}
	if q != s.query {
		s.query, s.sel = q, 0
		s.matches = filterCommands(q)
	}
}

// applySlash removes the typed "/query" and runs the selected command.
func (m *Model) applySlash() tea.Cmd {
	s := m.slash
	m.slash = slashMenu{}
	c := s.matches[s.sel]

	lines := strings.Split(m.editor.Value(), "\n")
	line := []rune(lines[s.row])
	end := min(m.editor.Column(), len(line))
	rest := append(append([]rune{}, line[:s.startCol]...), line[end:]...)
	col := s.startCol
	if c.isBlock && col == len(rest) {
		// "some text /h1" → "# some text", without the trailing space.
		for col > 0 && rest[col-1] == ' ' {
			col--
		}
		rest = rest[:col]
	}
	lines[s.row] = string(rest)

	row := s.row
	if c.isBlock {
		lines[row], col = applyBlock(lines[row], c.prefix, col)
	} else {
		lines, row, col = applyInsert(lines, row, col, c.insert(m.now()), c.ownLines)
	}
	m.setValueAt(strings.Join(lines, "\n"), row, col)
	return m.contentChanged()
}

// setValueAt replaces the buffer and puts the cursor at a logical (row,
// col). The textarea only moves by wrapped visual lines, so step down one
// logical line at a time from the end of each line's last wrapped segment.
func (m *Model) setValueAt(value string, row, col int) {
	m.editor.SetValue(value)
	m.editor.MoveToBegin()
	lines := strings.Split(value, "\n")
	for r := 0; r < row && r < len(lines)-1; r++ {
		m.editor.SetCursorColumn(len([]rune(lines[r])))
		m.editor.CursorDown()
	}
	m.editor.SetCursorColumn(col)
}

// contentChanged schedules a debounced preview render in split mode.
func (m *Model) contentChanged() tea.Cmd {
	if m.mode != modeSplit {
		return nil
	}
	m.renderSeq++
	return scheduleRender(m.renderSeq)
}

// ---------------------------------------------------------------- lists

// listRe matches a Markdown list marker: indent, bullet or number, spacing,
// optional task checkbox.
var listRe = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])(\s+)(\[[ xX]\]\s+)?`)

// continueList handles Enter on a list item. It returns false when the
// regular newline behaviour should apply.
func (m *Model) continueList() bool {
	if m.editor.HasSelection() {
		return false
	}
	lines := strings.Split(m.editor.Value(), "\n")
	row := m.editor.Line()
	if row < 0 || row >= len(lines) {
		return false
	}
	line := []rune(lines[row])
	loc := listRe.FindStringSubmatchIndex(string(line))
	if loc == nil {
		return false
	}
	markerRunes := len([]rune(string(line)[:loc[1]]))
	col := m.editor.Column()
	if col < markerRunes {
		return false
	}

	// Empty item: Enter ends the list by clearing the marker.
	if strings.TrimSpace(string(line[markerRunes:])) == "" && col == len(line) {
		for range len(line) {
			m.editor, _ = m.editor.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
		return true
	}

	s := string(line)
	indent, bullet, gap := s[loc[2]:loc[3]], s[loc[4]:loc[5]], s[loc[6]:loc[7]]
	if n, err := strconv.Atoi(strings.TrimRight(bullet, ".)")); err == nil {
		bullet = strconv.Itoa(n+1) + bullet[len(bullet)-1:]
	}
	next := indent + bullet + gap
	if loc[8] >= 0 {
		next += "[ ] "
	}
	m.editor.InsertString("\n" + next)
	return true
}

// ---------------------------------------------------------------- saving

func (m Model) save() (tea.Model, tea.Cmd) {
	m.slash = slashMenu{}
	if m.doc.Path == "" {
		m.prompt = promptSaveAs
		m.input.SetValue("")
		return m, m.input.Focus()
	}
	if err := m.doc.Save(m.editor.Value()); err != nil {
		m.quitAfterSave = false
		return m, m.setStatus("Save failed: "+err.Error(), true)
	}
	if m.quitAfterSave {
		return m, tea.Quit
	}
	return m, m.setStatus("Saved "+m.doc.Path, false)
}

func (m Model) handleSaveAsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.prompt = promptNone
		m.quitAfterSave = false
		m.input.Blur()
		return m, nil
	case "enter":
		p := strings.TrimSpace(m.input.Value())
		if p == "" {
			return m, nil
		}
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		if filepath.Ext(p) == "" {
			p += ".md"
		}
		if _, err := os.Stat(p); err == nil {
			m.prompt = promptNone
			m.input.Blur()
			return m, m.setStatus(p+" already exists; not overwriting", true)
		}
		m.prompt = promptNone
		m.input.Blur()
		m.doc.Path = p
		return m.save()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------- view

func (m Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.doc.Name() + " — mdn"
	if m.width == 0 {
		return v
	}

	var body string
	switch {
	case m.showHelp:
		box := m.st.helpBox.Render(
			lipgloss.NewStyle().Bold(true).Render("mdn — keyboard shortcuts") + "\n\n" +
				m.help.FullHelpView(m.keys.FullHelp()) + "\n\n" +
				m.help.Styles.FullKey.Render("/") + " " +
				m.help.Styles.FullDesc.Render("type / for commands (/h2, /todo, /code, /table…)") + "\n\n" +
				m.help.Styles.ShortDesc.Render("press any key to close"))
		body = lipgloss.Place(m.width, m.bodyHeight(), lipgloss.Center, lipgloss.Center, box)
	case m.mode == modeEdit:
		body = m.editor.View()
	case m.mode == modeRead:
		body = m.preview.View()
	default:
		edW, pvW := m.paneWidths()
		h := m.bodyHeight()
		div := m.st.divider.Render(strings.TrimSuffix(strings.Repeat("│\n", h), "\n"))
		left := lipgloss.NewStyle().Width(edW).Height(h).MaxHeight(h).Render(m.editor.View())
		right := lipgloss.NewStyle().Width(pvW).Height(h).MaxHeight(h).Render(m.preview.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, div, right)
	}
	body = lipgloss.NewStyle().Height(m.bodyHeight()).MaxHeight(m.bodyHeight()).Render(body)

	content := lipgloss.JoinVertical(lipgloss.Left, m.titleBar(), body, m.statusBar())
	if m.slash.active && !m.showHelp && m.prompt == promptNone && m.mode != modeRead {
		content = m.overlayMenu(content)
	}
	// Components have minimum sizes; never draw past the window.
	v.Content = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(content)
	return v
}

// overlayMenu draws the slash menu just below the "/" (or above it when
// there's no room), clamped to the editor pane.
func (m Model) overlayMenu(content string) string {
	ed := m.editor
	ed.SetVirtualCursor(false)
	cur := ed.Cursor()
	if cur == nil {
		return content
	}
	edW, _ := m.paneWidths()
	x := cur.Position.X - (m.editor.Column() - m.slash.startCol)
	y := cur.Position.Y + 1 // below the title bar

	// Rows between the title bar (row 0) and status bar (row height-1).
	below := m.height - 1 - (y + 1)
	above := y - 1
	box := renderMenu(m.st, m.slash, edW, max(below, above))
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	py := y + 1
	if bh > below && above > below {
		py = max(y-bh, 1)
	}
	px := max(min(x, edW-bw), 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(content),
		lipgloss.NewLayer(box).X(px).Y(py).Z(1),
	).Render()
}

// fill lays out left and right on one bar-styled line of the full width.
func (m Model) fill(left, right string) string {
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		right = ""
		gap = max(m.width-lipgloss.Width(left), 0)
	}
	line := left + m.st.bar.Render(strings.Repeat(" ", gap)) + right
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

func (m Model) titleBar() string {
	left := m.st.badge[m.mode].Render(m.mode.String()) + m.st.title.Render(m.doc.Name())
	if m.doc.Dirty(m.editor.Value()) {
		left += m.st.dirty.Render("● ")
	}
	right := ""
	if m.doc.Path != "" {
		if dir := filepath.Dir(m.doc.Path); dir != "." {
			right = m.st.status.Render(dir)
		}
	}
	return m.fill(left, right)
}

func (m Model) statusBar() string {
	hint := m.st.hint.Render("cmd+g help")
	switch m.prompt {
	case promptSaveAs:
		return m.fill(m.st.bar.Render(" ")+m.input.View(), m.st.hint.Render("enter save · esc cancel"))
	case promptConfirmQuit:
		return m.fill(m.st.statusErr.Render("Unsaved changes!"),
			m.st.hint.Render("cmd+s save & quit · cmd+q discard · any key cancel"))
	}
	if m.slash.active && m.mode != modeRead {
		return m.fill(m.st.status.Render("/"+m.slash.query),
			m.st.hint.Render("↑↓ select · enter apply · esc close"))
	}
	if m.status != "" {
		st := m.st.statusMsg
		if m.statusErr {
			st = m.st.statusErr
		}
		return m.fill(st.Render(m.status), hint)
	}

	var info string
	if m.mode == modeRead {
		info = fmt.Sprintf("%3.0f%%", m.preview.ScrollPercent()*100)
	} else {
		info = fmt.Sprintf("Ln %d, Col %d", m.editor.Line()+1, m.editor.Column()+1)
	}
	words := len(strings.Fields(m.editor.Value()))
	info += fmt.Sprintf(" · %d words", words)
	return m.fill(m.st.status.Render(info), hint)
}
