package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"mdn/internal/copilot"
	"mdn/internal/textpos"
)

const (
	completionDelay = 150 * time.Millisecond
	tabSize         = 2 // matches the Indent key
	languageID      = "markdown"
)

// copilotState is the Copilot part of the Model.
type copilotState struct {
	client  *copilot.Client
	err     error // the server failed to start
	status  copilot.Status
	hasStat bool
	uri     string // document URI the server has open
	version int
	synced  string // buffer content the server knows about

	seq      int // debounce generation
	cancel   context.CancelFunc
	inflight bool

	ghosts         []Ghost
	gi             int
	shown          bool
	ghostRow, gCol int // cursor position the ghosts are anchored to

	signIn       *copilot.SignInResult
	signInURL    string
	signInCancel context.CancelFunc
}

type (
	cpReadyMsg    struct{ c *copilot.Client }
	cpErrMsg      struct{ err error }
	cpStatusMsg   copilot.Status
	cpNoticeMsg   string
	cpShowDocMsg  string
	cpDebounceMsg struct {
		seq     int
		invoked bool
	}
	cpCompletionMsg struct {
		version, row, col int
		items             []copilot.InlineCompletionItem
		err               error
	}
	signInStartedMsg struct {
		res copilot.SignInResult
		err error
	}
	signInDoneMsg struct{ err error }
)

func (m Model) copilotEnabled() bool { return m.opts.StartCopilot != nil }

// docURI is the URI reported for the buffer; untitled buffers get a
// placeholder in the working directory.
func (m Model) docURI() string {
	p := m.doc.Path
	if p == "" {
		cwd, _ := os.Getwd()
		p = filepath.Join(cwd, "untitled.md")
	}
	return copilot.FileURI(p)
}

// startCopilot launches the language server in the background.
func (m Model) startCopilot() tea.Cmd {
	if !m.copilotEnabled() {
		return nil
	}
	start, send, logger := m.opts.StartCopilot, m.opts.Send, m.opts.Logger
	root := "."
	if m.doc.Path != "" {
		root = filepath.Dir(m.doc.Path)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c, err := start(ctx, copilot.Options{
			RootDir:        root,
			Logger:         logger,
			OpenBrowser:    true,
			OnStatus:       func(s copilot.Status) { send(cpStatusMsg(s)) },
			OnMessage:      func(s string) { send(cpNoticeMsg(s)) },
			OnShowDocument: func(u string) { send(cpShowDocMsg(u)) },
		})
		if err != nil {
			return cpErrMsg{err}
		}
		return cpReadyMsg{c}
	}
}

// Close shuts down the Copilot server. Call it after the program exits.
func (m Model) Close() {
	if m.cp.cancel != nil {
		m.cp.cancel()
	}
	if m.cp.signInCancel != nil {
		m.cp.signInCancel()
	}
	if c := m.cp.client; c != nil {
		_ = c.DidClose(m.cp.uri)
		c.Close()
	}
}

// updateCopilot handles Copilot messages; ok is false for anything else.
func (m Model) updateCopilot(msg tea.Msg) (_ Model, _ tea.Cmd, ok bool) {
	switch msg := msg.(type) {
	case cpReadyMsg:
		m.cp.client = msg.c
		m.cp.version = 1
		m.cp.synced = m.editor.Value()
		m.cp.uri = m.docURI()
		if err := msg.c.DidOpen(m.cp.uri, languageID, m.cp.version, m.cp.synced); err != nil {
			m.logf("didOpen: %v", err)
		}
		return m, nil, true

	case cpErrMsg:
		m.cp.err = msg.err
		m.logf("copilot unavailable: %v", msg.err)
		return m, m.setStatus("Copilot unavailable: "+msg.err.Error(), true), true

	case cpStatusMsg:
		m.cp.status, m.cp.hasStat = copilot.Status(msg), true
		return m, nil, true

	case cpNoticeMsg:
		return m, m.setStatus("Copilot: "+string(msg), false), true

	case cpShowDocMsg:
		m.cp.signInURL = string(msg)
		return m, nil, true

	case cpDebounceMsg:
		if msg.seq != m.cp.seq {
			return m, nil, true
		}
		return m, m.requestCompletion(msg.invoked), true

	case cpCompletionMsg:
		return m, m.handleCompletion(msg), true

	case signInStartedMsg:
		return m, m.handleSignInStarted(msg), true

	case signInDoneMsg:
		m.cp.signIn, m.cp.signInURL = nil, ""
		if m.cp.signInCancel != nil {
			m.cp.signInCancel()
			m.cp.signInCancel = nil
		}
		switch {
		case errors.Is(msg.err, context.Canceled):
			return m, m.setStatus("Copilot sign-in cancelled", false), true
		case msg.err != nil:
			return m, m.setStatus("Copilot sign-in failed: "+msg.err.Error(), true), true
		}
		return m, m.setStatus("Signed in to GitHub Copilot", false), true
	}
	return m, nil, false
}

func (m Model) logf(format string, args ...any) {
	if m.opts.Logger != nil {
		m.opts.Logger.Printf(format, args...)
	}
}

// ---------------------------------------------------------------- sync

// syncCopilot sends any buffer change since the last sync to the server.
func (m *Model) syncCopilot() {
	c := m.cp.client
	cur := m.editor.Value()
	if c == nil || cur == m.cp.synced {
		return
	}
	m.cp.version++
	if err := c.DidChange(m.cp.uri, m.cp.version, m.cp.synced, cur); err != nil {
		m.logf("didChange: %v", err)
	}
	m.cp.synced = cur
}

// reopenIfMoved re-registers the document after "save as" changes its URI.
func (m *Model) reopenIfMoved() {
	c := m.cp.client
	if c == nil || m.docURI() == m.cp.uri {
		return
	}
	m.clearGhosts()
	m.cancelCompletion()
	_ = c.DidClose(m.cp.uri)
	m.cp.uri = m.docURI()
	m.cp.version++
	m.cp.synced = m.editor.Value()
	if err := c.DidOpen(m.cp.uri, languageID, m.cp.version, m.cp.synced); err != nil {
		m.logf("didOpen: %v", err)
	}
}

// advanceGhost handles typing that matches the suggestion: if the edit from
// before (with the cursor at row, col) inserted exactly the next part of the
// ghost, the ghost shrinks instead of being discarded.
func (m *Model) advanceGhost(before string, row, col int) bool {
	g, ok := m.ghostAt(row, col)
	if !ok {
		return false
	}
	cur := m.editor.Value()
	start, oldEnd, newEnd := textpos.Diff(before, cur)
	if oldEnd != start || start != byteOffset(before, row, col) {
		return false
	}
	ng, ok := g.Advance(cur[start:newEnd])
	if !ok {
		return false
	}
	m.syncCopilot()
	m.setGhosts([]Ghost{ng})
	m.cp.shown = true
	return true
}

// ---------------------------------------------------------------- requests

// scheduleCompletion debounces an automatic completion request.
func (m *Model) scheduleCompletion() tea.Cmd {
	m.cancelCompletion()
	if m.cp.client == nil {
		return nil
	}
	m.cp.seq++
	seq := m.cp.seq
	return tea.Tick(completionDelay, func(time.Time) tea.Msg { return cpDebounceMsg{seq: seq} })
}

func (m *Model) cancelCompletion() {
	if m.cp.cancel != nil {
		m.cp.cancel()
		m.cp.cancel = nil
	}
	m.cp.inflight = false
}

// requestCompletion asks for suggestions at the cursor. invoked is true when
// the user explicitly asked.
func (m *Model) requestCompletion(invoked bool) tea.Cmd {
	m.cancelCompletion()
	c := m.cp.client
	if c == nil {
		switch {
		case !invoked:
		case !m.copilotEnabled():
			return m.setStatus("Copilot is disabled (--no-copilot)", true)
		case m.cp.err != nil:
			return m.setStatus("Copilot unavailable: "+m.cp.err.Error(), true)
		default:
			return m.setStatus("Copilot is still starting…", false)
		}
		return nil
	}
	if m.mode == modeRead || m.slash.active || m.editor.HasSelection() {
		return nil
	}
	if !invoked && m.cp.hasStat && m.cp.status.Kind == copilot.StatusError {
		return nil // not signed in; don't spam the server
	}
	m.syncCopilot()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.cp.cancel = cancel
	m.cp.inflight = true

	uri, version := m.cp.uri, m.cp.version
	row, col := m.editor.Line(), m.editor.Column()
	pos := copilot.Position{Line: row, Character: textpos.RuneColToUTF16(lineAt(m.editor.Value(), row), col)}
	return func() tea.Msg {
		items, err := c.InlineCompletion(ctx, uri, version, pos, tabSize, invoked)
		return cpCompletionMsg{version: version, row: row, col: col, items: items, err: err}
	}
}

func (m *Model) handleCompletion(msg cpCompletionMsg) tea.Cmd {
	if msg.version != m.cp.version || msg.row != m.editor.Line() || msg.col != m.editor.Column() {
		return nil // stale
	}
	m.cp.inflight = false
	m.cp.cancel = nil
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return nil
		}
		m.logf("inlineCompletion: %v", msg.err)
		hint := ""
		if s := strings.ToLower(msg.err.Error()); strings.Contains(s, "sign") || strings.Contains(s, "auth") {
			hint = " (cmd+l to sign in)"
		}
		return m.setStatus("Copilot: "+msg.err.Error()+hint, true)
	}
	if m.mode == modeRead || m.slash.active {
		return nil
	}

	lines := strings.Split(m.editor.Value(), "\n")
	var ghosts []Ghost
	seen := map[string]bool{}
	for _, it := range msg.items {
		g, ok := MakeGhost(lines, msg.row, msg.col, it)
		if !ok || seen[g.Text] {
			continue
		}
		seen[g.Text] = true
		ghosts = append(ghosts, g)
	}
	m.logf("completion: %d items, %d usable", len(msg.items), len(ghosts))
	m.setGhosts(ghosts)
	m.markShown()
	return nil
}

// ---------------------------------------------------------------- ghosts

func (m *Model) setGhosts(gs []Ghost) {
	m.cp.ghosts, m.cp.gi, m.cp.shown = gs, 0, false
	m.cp.ghostRow, m.cp.gCol = m.editor.Line(), m.editor.Column()
}

func (m *Model) clearGhosts() { m.setGhosts(nil) }

// ghostAt returns the current suggestion if it is anchored at (row, col).
func (m Model) ghostAt(row, col int) (Ghost, bool) {
	if len(m.cp.ghosts) == 0 || m.cp.ghostRow != row || m.cp.gCol != col {
		return Ghost{}, false
	}
	return m.cp.ghosts[m.cp.gi], true
}

// ghost returns the suggestion to display at the cursor, if any.
func (m Model) ghost() (Ghost, bool) {
	if m.mode == modeRead || m.slash.active || m.cp.signIn != nil || m.editor.HasSelection() {
		return Ghost{}, false
	}
	return m.ghostAt(m.editor.Line(), m.editor.Column())
}

func (m *Model) cycleGhost(delta int) {
	if n := len(m.cp.ghosts); n > 1 {
		m.cp.gi = (m.cp.gi + delta + n) % n
		m.cp.shown = false
		m.markShown()
	}
}

func (m *Model) markShown() {
	g, ok := m.ghost()
	if !ok || m.cp.shown || m.cp.client == nil {
		return
	}
	m.cp.shown = true
	if err := m.cp.client.DidShow(g.Item); err != nil {
		m.logf("didShowCompletion: %v", err)
	}
}

// acceptGhost inserts the whole suggestion.
func (m *Model) acceptGhost(g Ghost) tea.Cmd {
	m.clearGhosts()
	m.cancelCompletion()
	for range g.DeleteAfter {
		m.editor, _ = m.editor.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	}
	m.editor.InsertString(g.Text)
	cmd := m.contentChanged()
	c := m.cp.client
	if c == nil {
		return cmd
	}
	logger := m.opts.Logger
	return tea.Batch(cmd, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.DidAccept(ctx, g.Item); err != nil && logger != nil {
			logger.Printf("accept command: %v", err)
		}
		return nil
	})
}

// acceptWord inserts the next word of the suggestion and keeps the rest.
func (m *Model) acceptWord(g Ghost) tea.Cmd {
	w := NextWord(g.Text)
	if w == "" || w == g.Text {
		return m.acceptGhost(g)
	}
	m.cancelCompletion()
	for range g.DeleteAfter {
		m.editor, _ = m.editor.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	}
	g.DeleteAfter = 0
	m.editor.InsertString(w)
	m.syncCopilot()
	g.Text = g.Text[len(w):]
	m.setGhosts([]Ghost{g})
	m.cp.shown = true
	if c := m.cp.client; c != nil {
		if err := c.DidPartiallyAccept(g.Item, g.AcceptedLength()); err != nil {
			m.logf("didPartiallyAccept: %v", err)
		}
	}
	return m.previewChanged()
}

// spliceGhost overlays the current suggestion on the rendered editor.
func (m Model) spliceGhost(view string) string {
	g, ok := m.ghost()
	if !ok {
		return view
	}
	cur := m.editor.Cursor()
	if cur == nil {
		return view
	}
	gutter := cur.X - m.editor.LineInfo().CharOffset
	line := []rune(lineAt(m.editor.Value(), m.editor.Line()))
	col := min(m.editor.Column(), len(line))
	skip := ansi.StringWidth(string(line[col:min(len(line), col+g.DeleteAfter)]))
	edW, _ := m.paneWidths()
	return SpliceGhost(view, cur.X, cur.Y, gutter, skip, edW, g.Text,
		func(s string) string { return m.st.ghost.Render(s) })
}

// ---------------------------------------------------------------- sign-in

func (m *Model) startSignIn() tea.Cmd {
	c := m.cp.client
	switch {
	case !m.copilotEnabled():
		return m.setStatus("Copilot is disabled (--no-copilot)", true)
	case m.cp.err != nil:
		return m.setStatus("Copilot unavailable: "+m.cp.err.Error(), true)
	case c == nil:
		return m.setStatus("Copilot is still starting…", false)
	}
	m.slash = slashMenu{}
	return tea.Batch(m.setStatus("Contacting GitHub…", false), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := c.SignIn(ctx)
		return signInStartedMsg{res, err}
	})
}

func (m *Model) handleSignInStarted(msg signInStartedMsg) tea.Cmd {
	if msg.err != nil {
		return m.setStatus("Copilot sign-in failed: "+msg.err.Error(), true)
	}
	if msg.res.AlreadySignedIn() {
		who := msg.res.User
		if who == "" {
			who = "GitHub"
		}
		return m.setStatus("Already signed in to Copilot as "+who, false)
	}
	res := msg.res
	m.clearGhosts()
	m.cp.signIn = &res
	m.cp.signInURL = res.VerificationURI
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	m.cp.signInCancel = cancel
	c := m.cp.client
	return tea.Batch(
		tea.SetClipboard(res.UserCode),
		func() tea.Msg { return signInDoneMsg{c.FinishSignIn(ctx, res)} },
	)
}

// handleSignInKey handles keys while the device-flow box is showing.
func (m Model) handleSignInKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" && m.cp.signInCancel != nil {
		m.cp.signInCancel()
	}
	return m, nil
}

func (m Model) signInView() string {
	url := m.cp.signInURL
	if url == "" {
		url = "https://github.com/login/device"
	}
	bold := m.st.title.UnsetBackground().UnsetPadding()
	return m.st.helpBox.Render(strings.Join([]string{
		bold.Render("Sign in to GitHub Copilot"),
		"",
		"Your one-time code:  " + m.st.code.Render(m.cp.signIn.UserCode) + "  (copied to clipboard)",
		"",
		"Enter it at " + m.st.link.Render(url),
		"(a browser window should open automatically)",
		"",
		m.help.Styles.ShortDesc.Render("Waiting for authorization…  esc to cancel"),
	}, "\n"))
}

// ---------------------------------------------------------------- status

func (m Model) copilotIndicator() string {
	var s string
	ok, bad, plain := m.st.cpOK, m.st.cpBad, m.st.hint
	switch {
	case !m.copilotEnabled():
		return ""
	case m.cp.err != nil:
		s = bad.Render("Copilot ✗")
	case m.cp.client == nil:
		s = plain.Render("Copilot …")
	case m.cp.hasStat && m.cp.status.Kind == copilot.StatusError:
		s = bad.Render("Copilot ✗ cmd+l sign in")
	case m.cp.hasStat && m.cp.status.Kind == copilot.StatusWarning:
		s = bad.Render("Copilot ⚠")
	case m.cp.hasStat && m.cp.status.Kind == copilot.StatusInactive:
		s = plain.Render("Copilot ○")
	case m.cp.inflight || (m.cp.hasStat && m.cp.status.Busy):
		s = ok.Render("Copilot ⋯")
	default:
		s = ok.Render("Copilot ✓")
	}
	if n := len(m.cp.ghosts); n > 1 {
		if _, shown := m.ghost(); shown {
			s = plain.Render(fmt.Sprintf("%d/%d", m.cp.gi+1, n)) + s
		}
	}
	return s
}

// ---------------------------------------------------------------- helpers

func lineAt(text string, row int) string {
	for range row {
		j := strings.IndexByte(text, '\n')
		if j < 0 {
			return ""
		}
		text = text[j+1:]
	}
	if j := strings.IndexByte(text, '\n'); j >= 0 {
		return text[:j]
	}
	return text
}

// byteOffset returns the byte offset in text of rune column col on line row.
func byteOffset(text string, row, col int) int {
	off := 0
	for range row {
		j := strings.IndexByte(text[off:], '\n')
		if j < 0 {
			return len(text)
		}
		off += j + 1
	}
	for i := 0; i < col && off < len(text) && text[off] != '\n'; i++ {
		_, size := utf8.DecodeRuneInString(text[off:])
		off += size
	}
	return off
}
