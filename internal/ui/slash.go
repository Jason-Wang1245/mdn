package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// cursorMark marks where the cursor goes inside an insert template.
const cursorMark = "‸"

// slashCmd is one entry in the "/" command menu.
type slashCmd struct {
	name    string   // what you type after "/"
	title   string   // shown in the menu
	preview string   // Markdown it produces, shown dimmed
	aliases []string // alternative names for filtering

	// Block commands replace the current line's block prefix.
	isBlock bool
	prefix  string

	// Insert commands insert a template at the cursor. ownLines puts the
	// template on its own line(s), with a blank line before it if needed.
	insert   func(now time.Time) string
	ownLines bool
}

func block(name, title, prefix string, aliases ...string) slashCmd {
	preview := strings.TrimSpace(prefix)
	if preview == "" {
		preview = "¶"
	}
	return slashCmd{name: name, title: title, preview: preview, aliases: aliases, isBlock: true, prefix: prefix}
}

func insert(name, title, preview string, ownLines bool, tmpl string, aliases ...string) slashCmd {
	return slashCmd{
		name: name, title: title, preview: preview, aliases: aliases, ownLines: ownLines,
		insert: func(time.Time) string { return tmpl },
	}
}

func stamp(name, title, layout string, aliases ...string) slashCmd {
	return slashCmd{
		name: name, title: title, preview: time.Date(2006, 1, 2, 15, 4, 0, 0, time.UTC).Format(layout),
		aliases: aliases, insert: func(now time.Time) string { return now.Format(layout) },
	}
}

var slashCommands = []slashCmd{
	block("h1", "Heading 1", "# ", "heading", "title"),
	block("h2", "Heading 2", "## ", "heading2", "subtitle"),
	block("h3", "Heading 3", "### ", "heading3"),
	block("h4", "Heading 4", "#### ", "heading4"),
	block("h5", "Heading 5", "##### ", "heading5"),
	block("h6", "Heading 6", "###### ", "heading6"),
	block("text", "Text", "", "paragraph", "plain"),
	block("bullet", "Bulleted list", "- ", "list", "ul"),
	block("numbered", "Numbered list", "1. ", "ol", "number"),
	block("todo", "To-do", "- [ ] ", "task", "checkbox"),
	block("quote", "Quote", "> ", "blockquote"),
	block("callout", "Callout", "> [!note] ", "note", "admonition"),
	insert("code", "Code block", "```", true, "```"+cursorMark+"\n\n```", "codeblock", "fence"),
	insert("table", "Table", "| |", true, "| "+cursorMark+" |  |  |\n| --- | --- | --- |\n|  |  |  |"),
	insert("divider", "Divider", "---", true, "---\n"+cursorMark, "hr", "rule", "line"),
	insert("link", "Link", "[]()", false, "["+cursorMark+"](url)", "url"),
	insert("image", "Image", "![]()", false, "!["+cursorMark+"](url)", "img", "picture"),
	insert("math", "Math block", "$$", true, "$$\n"+cursorMark+"\n$$", "latex", "equation"),
	insert("details", "Collapsible", "<details>", true,
		"<details>\n<summary>"+cursorMark+"</summary>\n\n</details>", "toggle", "collapse"),
	stamp("date", "Today's date", "2006-01-02", "today"),
	stamp("time", "Current time", "15:04", "clock"),
	stamp("now", "Date & time", "2006-01-02 15:04", "datetime", "timestamp"),
}

// filterCommands ranks commands for query: exact name/alias, then name
// prefix, then alias prefix, then subsequence matches on name/alias/title.
func filterCommands(query string) []slashCmd {
	q := strings.ToLower(query)
	if q == "" {
		return slices(slashCommands)
	}
	type ranked struct {
		cmd   slashCmd
		score int
	}
	var out []ranked
	for _, c := range slashCommands {
		score := -1
		names := append([]string{c.name}, c.aliases...)
		for i, n := range names {
			switch {
			case n == q:
				score = best(score, 0)
			case strings.HasPrefix(n, q) && i == 0:
				score = best(score, 1)
			case strings.HasPrefix(n, q):
				score = best(score, 2)
			case subsequence(q, n):
				score = best(score, 3)
			}
		}
		if score < 0 && subsequence(q, strings.ToLower(c.title)) {
			score = 4
		}
		if score >= 0 {
			out = append(out, ranked{c, score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score < out[j].score })
	cmds := make([]slashCmd, len(out))
	for i, r := range out {
		cmds[i] = r.cmd
	}
	return cmds
}

func slices(s []slashCmd) []slashCmd { return append([]slashCmd(nil), s...) }

func best(cur, s int) int {
	if cur < 0 || s < cur {
		return s
	}
	return cur
}

func subsequence(q, s string) bool {
	i := 0
	for _, r := range s {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}

// blockPrefixRe matches an existing block prefix: indent, then a heading,
// task, bullet, number, quote or callout marker.
var blockPrefixRe = regexp.MustCompile(`^(\s*)(#{1,6}\s+|[-*+]\s+\[[ xX]\]\s+|[-*+]\s+|\d+[.)]\s+|>\s?(?:\[![^\]]*\][+-]?\s*)?)?`)

// applyBlock replaces line's block prefix with prefix, keeping indentation
// and text. col and the returned column are rune offsets.
func applyBlock(line, prefix string, col int) (string, int) {
	loc := blockPrefixRe.FindStringSubmatchIndex(line)
	indent := line[loc[2]:loc[3]]
	rest := line[loc[1]:]
	oldLen := len([]rune(line[:loc[1]]))
	newLen := len([]rune(indent + prefix))
	return indent + prefix + rest, newLen + max(col-oldLen, 0)
}

// applyInsert inserts tmpl at (row, col) in lines and returns the new lines
// and the cursor position marked by cursorMark (or the end of the insert).
func applyInsert(lines []string, row, col int, tmpl string, ownLines bool) ([]string, int, int) {
	r := []rune(lines[row])
	col = min(col, len(r))
	before, after := string(r[:col]), string(r[col:])

	if !strings.Contains(tmpl, cursorMark) {
		tmpl += cursorMark
	}
	if ownLines {
		prevBlank := strings.TrimSpace(before) == "" && (row == 0 || strings.TrimSpace(lines[row-1]) == "")
		switch {
		case strings.TrimSpace(before) != "":
			tmpl = "\n\n" + tmpl
		case !prevBlank:
			before = ""
			tmpl = "\n" + tmpl
		default:
			before = ""
		}
		if strings.TrimSpace(after) != "" {
			tmpl += "\n"
		}
	}

	text := before + tmpl + after
	idx := strings.Index(text, cursorMark)
	head := text[:idx]
	text = head + text[idx+len(cursorMark):]

	newRow := row + strings.Count(head, "\n")
	newCol := len([]rune(head[strings.LastIndex(head, "\n")+1:]))

	out := append([]string{}, lines[:row]...)
	out = append(out, strings.Split(text, "\n")...)
	out = append(out, lines[row+1:]...)
	return out, newRow, newCol
}

// slashMenu is the state of the open "/" menu.
type slashMenu struct {
	active   bool
	row      int // line holding the "/"
	startCol int // rune column of the "/"
	query    string
	sel      int
	matches  []slashCmd
}

const menuRows = 8

// renderMenu draws the menu box no wider than maxW and no taller than maxH.
func renderMenu(st styles, menu slashMenu, maxW, maxH int) string {
	inner := min(30, max(maxW-2, 12))
	visible := max(1, min(menuRows, maxH-2))
	showCount := len(menu.matches) > visible && maxH-2 > 1
	var rows []string
	if len(menu.matches) == 0 {
		rows = append(rows, st.menuMuted.Width(inner).Render("No matching commands"))
	}
	if showCount && visible+1 > maxH-2 {
		visible-- // make room for the "n/total" line
	}
	start := max(0, min(menu.sel-visible+1, len(menu.matches)-visible))
	end := min(start+visible, len(menu.matches))
	for i := start; i < end; i++ {
		c := menu.matches[i]
		style, muted := st.menuItem, st.menuMuted
		if i == menu.sel {
			style, muted = st.menuSel, st.menuSelMuted
		}
		title := c.title
		pv := c.preview
		gap := inner - 2 - lipgloss.Width(title) - lipgloss.Width(pv)
		if gap < 1 {
			pv, gap = "", max(inner-2-lipgloss.Width(title), 1)
		}
		row := style.Render(" "+title+strings.Repeat(" ", gap)) + muted.Render(pv+" ")
		rows = append(rows, lipgloss.NewStyle().MaxWidth(inner).Render(row))
	}
	if n := len(menu.matches); n > visible && showCount {
		rows = append(rows, st.menuMuted.Width(inner).Render(fmt.Sprintf(" %d/%d", menu.sel+1, n)))
	}
	return st.menuBox.Render(strings.Join(rows, "\n"))
}
