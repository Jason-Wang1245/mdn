package ui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
)

type keyMap struct {
	Save       key.Binding
	ToggleRead key.Binding
	Split      key.Binding
	Quit       key.Binding
	Help       key.Binding
	Copy       key.Binding
	Cut        key.Binding
	Paste      key.Binding
	SelectAll  key.Binding
	Indent     key.Binding

	// Extend the selection to the line / document start and end.
	SelectLineStart key.Binding
	SelectLineEnd   key.Binding
	SelectDocStart  key.Binding
	SelectDocEnd    key.Binding
	Scroll          key.Binding

	// Copilot
	Accept         key.Binding
	AcceptWord     key.Binding
	NextSuggestion key.Binding
	PrevSuggestion key.Binding
	Suggest        key.Binding
	Dismiss        key.Binding
	SignIn         key.Binding
}

// cmd binds Cmd+k (reported as "super+k" by terminals that speak the kitty
// keyboard protocol, e.g. Ghostty, kitty, WezTerm) and Ctrl+k as a fallback
// for terminals that keep Cmd shortcuts for themselves (Terminal.app, iTerm2
// by default). Help always shows the Cmd form.
func cmd(k, desc string, extra ...string) key.Binding {
	keys := append([]string{"super+" + k, "ctrl+" + k}, extra...)
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp("cmd+"+k, desc))
}

// cmdOr is cmd with a different Ctrl fallback, for shortcuts whose Ctrl form
// is taken by line navigation: many terminals send Home/End as Ctrl+a/Ctrl+e,
// so those must stay line start/end.
func cmdOr(k, ctrl, desc string) key.Binding {
	return key.NewBinding(key.WithKeys("super+"+k, "ctrl+"+ctrl), key.WithHelp("cmd+"+k, desc))
}

func defaultKeys() keyMap {
	return keyMap{
		Save:       cmd("s", "save"),
		ToggleRead: cmd("r", "edit/read", "super+e"),
		Split:      cmd("\\", "split view"),
		Quit:       cmd("q", "quit"),
		Help:       cmd("g", "help", "f1"),
		Copy:       cmd("c", "copy"),
		Cut:        cmd("x", "cut"),
		Paste:      cmd("v", "paste"),
		SelectAll:  cmdOr("a", "o", "select all"),
		Indent:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "indent")),
		Scroll:     key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),

		// Shift plus the line/document navigation keys of editorKeys. On a
		// Mac keyboard shift+home/end is fn+shift+←/→.
		SelectLineStart: key.NewBinding(key.WithKeys("shift+home", "shift+super+left"), key.WithHelp("shift+home/end", "select to line start/end")),
		SelectLineEnd:   key.NewBinding(key.WithKeys("shift+end", "shift+super+right")),
		SelectDocStart:  key.NewBinding(key.WithKeys("ctrl+shift+home", "shift+super+up")),
		SelectDocEnd:    key.NewBinding(key.WithKeys("ctrl+shift+end", "shift+super+down")),

		Accept:         key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "accept suggestion")),
		AcceptWord:     key.NewBinding(key.WithKeys("ctrl+right", "alt+right"), key.WithHelp("ctrl+→", "accept word")),
		NextSuggestion: key.NewBinding(key.WithKeys("alt+]"), key.WithHelp("alt+] / alt+[", "cycle suggestions")),
		PrevSuggestion: key.NewBinding(key.WithKeys("alt+[")),
		Suggest:        key.NewBinding(key.WithKeys("ctrl+/", "ctrl+_"), key.WithHelp("ctrl+/", "suggest now")), // legacy terminals send ctrl+/ as ctrl+_
		Dismiss:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "dismiss suggestion")),
		SignIn:         cmd("l", "copilot sign-in"),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Save, k.ToggleRead, k.Split, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Save, k.Quit, k.Help},
		{k.ToggleRead, k.Split, k.Scroll},
		{k.Copy, k.Cut, k.Paste, k.SelectAll, k.SelectLineStart},
		{k.Indent},
	}
}

// CopilotHelp lists the Copilot suggestion keys.
func (k keyMap) CopilotHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Accept, k.AcceptWord, k.Dismiss},
		{k.NextSuggestion, k.Suggest, k.SignIn},
	}
}

// editorKeys adapts the textarea's emacs-ish defaults to standard editor
// conventions: cmd+a (ctrl+o) selects all and cmd/ctrl+g is help. Ctrl+a and
// Ctrl+e stay line start/end, since many terminals send those for Home/End.
func editorKeys() textarea.KeyMap {
	km := textarea.DefaultKeyMap()
	// macOS-style navigation: cmd+←/→ line start/end, cmd+↑/↓ doc start/end.
	km.LineStart = key.NewBinding(key.WithKeys("home", "ctrl+a", "super+left"))
	km.LineEnd = key.NewBinding(key.WithKeys("end", "ctrl+e", "super+right"))
	km.InputBegin = key.NewBinding(key.WithKeys("ctrl+home", "super+up", "alt+<"))
	km.InputEnd = key.NewBinding(key.WithKeys("ctrl+end", "super+down", "alt+>"))
	km.SelectAll = key.NewBinding(key.WithKeys("super+a", "ctrl+o"))
	km.Paste = key.NewBinding(key.WithKeys("super+v", "ctrl+v"))
	km.CopySelection = key.NewBinding(key.WithKeys("ctrl+shift+c"))
	return km
}
