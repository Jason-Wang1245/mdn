package textpos

import "testing"

func TestUTF16(t *testing.T) {
	line := "a😀b中c"
	if got := UTF16Len(line); got != 6 {
		t.Fatalf("UTF16Len = %d", got)
	}
	for runeCol, u16 := range []int{0, 1, 3, 4, 5, 6} {
		if got := RuneColToUTF16(line, runeCol); got != u16 {
			t.Errorf("RuneColToUTF16(%d) = %d, want %d", runeCol, got, u16)
		}
		if got := UTF16ToRuneCol(line, u16); got != runeCol {
			t.Errorf("UTF16ToRuneCol(%d) = %d, want %d", u16, got, runeCol)
		}
	}
	if got := UTF16ToRuneCol(line, 99); got != 5 {
		t.Errorf("clamp = %d", got)
	}
}

func TestLineCol(t *testing.T) {
	text := "ab\n😀x\n"
	cases := []struct{ off, line, char int }{
		{0, 0, 0}, {2, 0, 2}, {3, 1, 0}, {7, 1, 2}, {8, 1, 3}, {9, 2, 0},
	}
	for _, c := range cases {
		l, ch := LineCol(text, c.off)
		if l != c.line || ch != c.char {
			t.Errorf("LineCol(%d) = %d,%d want %d,%d", c.off, l, ch, c.line, c.char)
		}
	}
}

func TestDiff(t *testing.T) {
	cases := []struct {
		old, new string
		want     string // new[start:newEnd] replacing old[start:oldEnd]
		s, oe    int
	}{
		{"hello", "hello!", "!", 5, 5},
		{"hello", "hllo", "", 1, 2},
		{"abc", "abc", "", 3, 3},
		{"", "x", "x", 0, 0},
		{"aaa", "aaaa", "a", 3, 3},
		{"x😀y", "x😁y", "😁", 1, 5}, // shares first 3 bytes of the emoji
	}
	for _, c := range cases {
		s, oe, ne := Diff(c.old, c.new)
		if s != c.s || oe != c.oe || c.new[s:ne] != c.want {
			t.Errorf("Diff(%q,%q) = %d,%d,%q want %d,%d,%q", c.old, c.new, s, oe, c.new[s:ne], c.s, c.oe, c.want)
		}
		if got := c.old[:s] + c.new[s:ne] + c.old[oe:]; got != c.new {
			t.Errorf("Diff(%q,%q) does not reconstruct: %q", c.old, c.new, got)
		}
	}
}
