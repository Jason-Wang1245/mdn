package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"mdn/internal/copilot"
	"mdn/internal/textpos"
)

// Ghost is a Copilot suggestion projected onto the current cursor position.
type Ghost struct {
	Item copilot.InlineCompletionItem
	// Text is what remains to be inserted at the cursor.
	Text string
	// DeleteAfter is the number of runes after the cursor (on the cursor line)
	// that the suggestion replaces.
	DeleteAfter int
}

// AcceptedLength is the UTF-16 length of the part of Item.InsertText that is
// already in the buffer (typed or partially accepted).
func (g Ghost) AcceptedLength() int {
	return textpos.UTF16Len(g.Item.InsertText) - textpos.UTF16Len(g.Text)
}

// MakeGhost converts a completion item into ghost text for a cursor at
// (row, col) where col is a rune index. lines is the buffer split on "\n".
// It returns false if the item does not apply to the current buffer.
func MakeGhost(lines []string, row, col int, item copilot.InlineCompletionItem) (Ghost, bool) {
	if row < 0 || row >= len(lines) {
		return Ghost{}, false
	}
	cur := []rune(lines[row])
	if col > len(cur) {
		return Ghost{}, false
	}
	g := Ghost{Item: item, Text: item.InsertText}

	if r := item.Range; r != nil {
		if r.Start.Line > row || r.Start.Line < 0 || r.End.Line != row {
			// Only handle replacements that end on the cursor line.
			return Ghost{}, false
		}
		startCol := textpos.UTF16ToRuneCol(lines[r.Start.Line], r.Start.Character)
		if r.Start.Line == row && startCol > col {
			return Ghost{}, false
		}
		// Text already in the buffer between range start and the cursor.
		var existing strings.Builder
		for l := r.Start.Line; l <= row; l++ {
			lr := []rune(lines[l])
			from, to := 0, len(lr)
			if l == r.Start.Line {
				from = startCol
			}
			if l == row {
				to = col
			}
			existing.WriteString(string(lr[from:to]))
			if l < row {
				existing.WriteByte('\n')
			}
		}
		prefix := existing.String()
		if !strings.HasPrefix(item.InsertText, prefix) {
			return Ghost{}, false
		}
		g.Text = item.InsertText[len(prefix):]

		endCol := textpos.UTF16ToRuneCol(lines[row], r.End.Character)
		if endCol > col {
			g.DeleteAfter = endCol - col
		}
	}

	g.Text, g.DeleteAfter = trimRedundantSuffix(g.Text, string(cur[col:]), g.DeleteAfter)
	if g.Text == "" {
		return Ghost{}, false
	}
	return g, true
}

// trimRedundantSuffix handles the common case where a suggestion that does
// not replace the rest of the line ends with exactly that rest (e.g. a closing
// paren the editor already has). Treat that text as replaced so it is not
// duplicated on accept.
func trimRedundantSuffix(text, rest string, deleteAfter int) (string, int) {
	if deleteAfter > 0 || rest == "" || strings.TrimSpace(rest) == "" {
		return text, deleteAfter
	}
	if strings.HasSuffix(text, rest) && !strings.Contains(text, "\n") {
		return text, utf8.RuneCountInString(rest)
	}
	return text, deleteAfter
}

// Advance updates the ghost after the user typed typed at the cursor. It
// returns false if the typing diverged from the suggestion.
func (g Ghost) Advance(typed string) (Ghost, bool) {
	if typed == "" || !strings.HasPrefix(g.Text, typed) {
		return Ghost{}, false
	}
	g.Text = g.Text[len(typed):]
	if g.Text == "" {
		return Ghost{}, false
	}
	return g, true
}

// NextWord returns the next chunk to accept for a partial (word) accept:
// leading whitespace plus one word, or one run of punctuation. A leading
// newline is accepted together with the following indentation.
func NextWord(s string) string {
	if s == "" {
		return ""
	}
	i := 0
	if s[0] == '\n' {
		i = 1
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		return s[:i]
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) || s[i] == '\n' {
		return s[:i]
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	want := isWord(r)
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' || unicode.IsSpace(r) || isWord(r) != want {
			break
		}
		i += size
	}
	return s[:i]
}

// SpliceGhost overlays ghost text onto a rendered textarea view.
//
// cursorX/cursorY locate the cursor cell in view; gutter is the width of the
// prompt + line-number area (used to indent continuation lines); skipCols is
// the display width of buffer text after the cursor that the suggestion
// replaces; width/height bound the output; style renders ghost text.
func SpliceGhost(view string, cursorX, cursorY, gutter, skipCols, width int, ghost string, style func(string) string) string {
	lines := strings.Split(view, "\n")
	height := len(lines)
	if cursorY < 0 || cursorY >= height || ghost == "" {
		return view
	}
	line := lines[cursorY]
	lineW := ansi.StringWidth(line)
	left := ansi.Cut(line, 0, cursorX)
	if w := ansi.StringWidth(left); w < cursorX {
		left += strings.Repeat(" ", cursorX-w)
	}
	right := ""
	if cursorX+skipCols < lineW {
		right = ansi.Cut(line, cursorX+skipCols, lineW)
	}

	gl := strings.Split(ghost, "\n")
	out := make([]string, 0, height+len(gl))
	out = append(out, lines[:cursorY]...)
	pad := strings.Repeat(" ", max(gutter, 0))
	if len(gl) == 1 {
		out = append(out, fit(left+style(gl[0])+right, width))
	} else {
		out = append(out, fit(left+style(gl[0]), width))
		for _, g := range gl[1 : len(gl)-1] {
			out = append(out, fit(pad+style(g), width))
		}
		out = append(out, fit(pad+style(gl[len(gl)-1])+right, width))
	}
	out = append(out, lines[cursorY+1:]...)
	if len(out) > height {
		out = out[:height]
	}
	return strings.Join(out, "\n")
}

// fit truncates s to width display columns, trimming trailing blank padding
// first so overflowing padding never wraps the terminal.
func fit(s string, width int) string {
	if width <= 0 || ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}
