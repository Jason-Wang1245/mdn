package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
)

func TestFilterCommands(t *testing.T) {
	first := func(q string) string {
		got := filterCommands(q)
		if len(got) == 0 {
			return ""
		}
		return got[0].name
	}
	for q, want := range map[string]string{
		"": "h1", "h2": "h2", "todo": "todo", "task": "todo", "tab": "table",
		"cod": "code", "hr": "divider", "date": "date", "quo": "quote",
	} {
		if got := first(q); got != want {
			t.Errorf("filter %q: first = %q, want %q", q, got, want)
		}
	}
	if got := filterCommands("zzz"); len(got) != 0 {
		t.Errorf("zzz matched %d commands", len(got))
	}
}

func TestApplyBlock(t *testing.T) {
	cases := []struct {
		line, prefix, want string
		col, wantCol       int
	}{
		{"- buy milk", "## ", "## buy milk", 10, 11},
		{"### x", "", "x", 5, 1},
		{"  - [ ] a", "> ", "  > a", 9, 5},
		{"> [!tip] hi", "# ", "# hi", 11, 4},
		{"12. item", "- ", "- item", 0, 2},
		{"plain", "- [ ] ", "- [ ] plain", 2, 8},
	}
	for _, c := range cases {
		got, col := applyBlock(c.line, c.prefix, c.col)
		if got != c.want || col != c.wantCol {
			t.Errorf("applyBlock(%q,%q,%d) = %q,%d want %q,%d", c.line, c.prefix, c.col, got, col, c.want, c.wantCol)
		}
	}
}

func typed(t *testing.T, initial string, msgs ...tea.Msg) Model {
	t.Helper()
	m, _ := newTestModel(t, initial)
	m.now = func() time.Time { return time.Date(2026, 11, 17, 14, 5, 0, 0, time.Local) }
	if initial != "" {
		m.editor.MoveToEnd()
	}
	return send(m, msgs...)
}

func seq(parts ...any) []tea.Msg {
	var out []tea.Msg
	for _, p := range parts {
		switch p := p.(type) {
		case string:
			out = append(out, typeText(p)...)
		case tea.Msg:
			out = append(out, p)
		}
	}
	return out
}

func TestSlashEndToEnd(t *testing.T) {
	cases := []struct {
		name    string
		initial string
		msgs    []tea.Msg
		want    string
	}{
		{"h2 then type", "", seq("/h2", keyEnter, "Title"), "## Title"},
		{"trailing h1", "", seq("some text /h1", keyEnter), "# some text"},
		{"replace bullet", "- buy milk ", seq("/h3", keyEnter), "### buy milk"},
		{"tab applies", "", seq("/todo", tea.KeyPressMsg{Code: tea.KeyTab}, "x"), "- [ ] x"},
		{"arrow selection", "", seq("/h", keyDown, keyEnter, "x"), "## x"},
		{"code block", "", seq("/code", keyEnter, "go"), "```go\n\n```"},
		{"code after text", "para", seq("\n/code", keyEnter), "para\n\n```\n\n```"},
		{"table", "", seq("/table", keyEnter, "A"), "| A |  |  |\n| --- | --- | --- |\n|  |  |  |"},
		{"divider", "", seq("/divider", keyEnter, "next"), "---\nnext"},
		{"link inline", "see ", seq("/link", keyEnter, "docs"), "see [docs](url)"},
		{"date", "", seq("/date", keyEnter), "2026-11-17"},
		{"now", "", seq("/now", keyEnter), "2026-11-17 14:05"},
		{"esc keeps text", "", seq("/h2", keyEsc, keyEnter), "/h2\n"},
		{"mid-word slash ignored", "", seq("a/b", keyEnter), "a/b\n"},
		{"space closes", "", seq("/ x", keyEnter), "/ x\n"},
		{"no match enter is newline", "", seq("/zzz", keyEnter), "/zzz\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := typed(t, c.initial, c.msgs...)
			if got := m.editor.Value(); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
			if m.slash.active {
				t.Error("menu should be closed")
			}
		})
	}
}

func TestSlashMenuState(t *testing.T) {
	m := typed(t, "", seq("/")...)
	if !m.slash.active || len(m.slash.matches) != len(slashCommands) {
		t.Fatalf("menu should open with all commands: %+v", m.slash.active)
	}
	m = send(m, typeText("tod")...)
	if m.slash.query != "tod" || m.slash.matches[0].name != "todo" {
		t.Fatalf("query=%q first=%v", m.slash.query, m.slash.matches[0].name)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyBackspace},
		tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !m.slash.active || m.slash.query != "" {
		t.Fatal("backspacing to the slash should keep the menu open")
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.slash.active {
		t.Fatal("deleting the slash should close the menu")
	}
	m = send(m, typeText("/")...)
	m = send(m, ctrl('e'))
	if m.slash.active {
		t.Fatal("read mode should close the menu")
	}
}

func TestSlashMenuRenders(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {20, 5}, {40, 10}} {
		m := typed(t, strings.Repeat("line\n", 30), tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = send(m, typeText("/h")...)
		content := m.View().Content
		if sz[0] >= 40 && !strings.Contains(plain(m), "Heading 2") {
			t.Errorf("%v: menu not drawn:\n%s", sz, plain(m))
		}
		if h, w := lipgloss.Height(content), lipgloss.Width(content); h > sz[1] || w > sz[0] {
			t.Errorf("%v: view is %dx%d", sz, w, h)
		}
	}
}

func TestSetValueAtWrappedLines(t *testing.T) {
	m, _ := newTestModel(t, "")
	m = send(m, tea.WindowSizeMsg{Width: 30, Height: 20})
	long := strings.Repeat("word ", 30)
	value := long + "\nshort\n" + long + "\ntarget line"
	m.setValueAt(value, 3, 4)
	if m.editor.Line() != 3 || m.editor.Column() != 4 {
		t.Fatalf("cursor at %d:%d, want 3:4", m.editor.Line(), m.editor.Column())
	}
}
