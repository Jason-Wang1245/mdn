package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"mdn/internal/copilot"
)

func rng(sl, sc, el, ec int) *copilot.Range {
	return &copilot.Range{Start: copilot.Position{Line: sl, Character: sc}, End: copilot.Position{Line: el, Character: ec}}
}

func TestMakeGhost(t *testing.T) {
	lines := []string{"package main", "", "func fib(n int) int {", "\tif n"}
	tests := []struct {
		name       string
		row, col   int
		item       copilot.InlineCompletionItem
		ok         bool
		text       string
		deleteAftr int
	}{
		{
			name: "range from line start", row: 3, col: 5,
			item: copilot.InlineCompletionItem{InsertText: "\tif n < 2 {\n\t\treturn n\n\t}", Range: rng(3, 0, 3, 5)},
			ok:   true, text: " < 2 {\n\t\treturn n\n\t}",
		},
		{
			name: "no range inserts at cursor", row: 3, col: 5,
			item: copilot.InlineCompletionItem{InsertText: " <= 1"},
			ok:   true, text: " <= 1",
		},
		{
			name: "mismatched prefix rejected", row: 3, col: 5,
			item: copilot.InlineCompletionItem{InsertText: "\twhile x", Range: rng(3, 0, 3, 5)},
		},
		{
			name: "range on other line rejected", row: 3, col: 5,
			item: copilot.InlineCompletionItem{InsertText: "x", Range: rng(2, 0, 2, 3)},
		},
		{
			name: "fully typed already", row: 3, col: 5,
			item: copilot.InlineCompletionItem{InsertText: "\tif n", Range: rng(3, 0, 3, 5)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, ok := MakeGhost(lines, tc.row, tc.col, tc.item)
			if ok != tc.ok {
				t.Fatalf("ok = %v", ok)
			}
			if !ok {
				return
			}
			if g.Text != tc.text || g.DeleteAfter != tc.deleteAftr {
				t.Fatalf("got %q/%d want %q/%d", g.Text, g.DeleteAfter, tc.text, tc.deleteAftr)
			}
		})
	}
}

func TestMakeGhostReplacesRestOfLine(t *testing.T) {
	lines := []string{"print(😀)"}
	// cursor after "print(" — rune col 6; range end covers ")" (UTF-16 col 9)
	item := copilot.InlineCompletionItem{InsertText: "print(😀, x)", Range: rng(0, 0, 0, 9)}
	// existing text up to the cursor is "print(" but insertText continues with 😀 — cursor at col 7 (after emoji)
	g, ok := MakeGhost(lines, 0, 7, item)
	if !ok || g.Text != ", x)" || g.DeleteAfter != 1 {
		t.Fatalf("got %+v ok=%v", g, ok)
	}
	if g.AcceptedLength() != len("print(")+2 {
		t.Fatalf("AcceptedLength = %d", g.AcceptedLength())
	}
}

func TestMakeGhostRedundantClosingParen(t *testing.T) {
	g, ok := MakeGhost([]string{"fib()"}, 0, 4, copilot.InlineCompletionItem{InsertText: "n - 1)"})
	if !ok || g.Text != "n - 1)" || g.DeleteAfter != 1 {
		t.Fatalf("got %+v", g)
	}
}

func TestAdvance(t *testing.T) {
	g := Ghost{Text: "return n"}
	g2, ok := g.Advance("ret")
	if !ok || g2.Text != "urn n" {
		t.Fatalf("got %+v %v", g2, ok)
	}
	if _, ok := g.Advance("x"); ok {
		t.Fatal("divergent typing should drop ghost")
	}
	if _, ok := g.Advance("return n"); ok {
		t.Fatal("fully typed ghost should be dropped")
	}
}

func TestNextWord(t *testing.T) {
	cases := map[string]string{
		" < 2 {":         " <",
		"foo(bar)":       "foo",
		"(bar)":          "(",
		"\n\t\treturn n": "\n\t\t",
		"  \nx":          "  ",
		"héllo wörld":    "héllo",
	}
	for in, want := range cases {
		if got := NextWord(in); got != want {
			t.Errorf("NextWord(%q) = %q want %q", in, got, want)
		}
	}
}

func TestSpliceGhost(t *testing.T) {
	view := strings.Join([]string{
		"   1 \x1b[1mfoo(\x1b[0m)   ",
		"   2 bar         ",
		"   3             ",
	}, "\n")
	style := func(s string) string { return "<" + s + ">" }

	// Single-line ghost at cursor after "foo(" (x=9), skipping nothing.
	out := SpliceGhost(view, 9, 0, 5, 0, 40, "x", style)
	first := ansi.Strip(strings.Split(out, "\n")[0])
	if first != "   1 foo(<x>)   " {
		t.Fatalf("single: %q", first)
	}

	// Multi-line ghost replacing ")" (skip 1 col): rest of line follows last ghost line.
	out = SpliceGhost(view, 9, 0, 5, 1, 40, "a,\nb)", style)
	got := strings.Split(ansi.Strip(out), "\n")
	want := []string{"   1 foo(<a,>", "     <b)>   ", "   2 bar         "}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("multi:\n%q\nwant\n%q", got, want)
	}

	// Width truncation.
	out = SpliceGhost(view, 9, 2, 5, 0, 12, "abcdefghij", style)
	last := strings.Split(out, "\n")[2]
	if w := ansi.StringWidth(last); w > 12 {
		t.Fatalf("width %d > 12: %q", w, ansi.Strip(last))
	}
}
