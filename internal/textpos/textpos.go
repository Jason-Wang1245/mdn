// Package textpos converts between rune columns, byte offsets and LSP
// UTF-16 positions.
package textpos

import (
	"strings"
	"unicode/utf8"
)

// UTF16Len returns the number of UTF-16 code units needed to encode s.
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// RuneColToUTF16 converts a rune index within line to a UTF-16 offset.
func RuneColToUTF16(line string, runeCol int) int {
	n, i := 0, 0
	for _, r := range line {
		if i >= runeCol {
			break
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		i++
	}
	return n
}

// UTF16ToRuneCol converts a UTF-16 offset within line to a rune index,
// clamping to the line length.
func UTF16ToRuneCol(line string, u16 int) int {
	n, i := 0, 0
	for _, r := range line {
		if n >= u16 {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		i++
	}
	return i
}

// LineCol returns the zero-based line and UTF-16 character position of the
// byte offset off within text.
func LineCol(text string, off int) (line, char int) {
	if off > len(text) {
		off = len(text)
	}
	prefix := text[:off]
	line = strings.Count(prefix, "\n")
	lastNL := strings.LastIndexByte(prefix, '\n')
	return line, UTF16Len(prefix[lastNL+1:])
}

// Diff returns the smallest single replacement turning old into new: the byte
// range [start, oldEnd) in old is replaced by new[start:newEnd]. Offsets are on
// rune boundaries.
func Diff(old, new string) (start, oldEnd, newEnd int) {
	// Common prefix.
	max := min(len(old), len(new))
	for start < max && old[start] == new[start] {
		start++
	}
	for start > 0 && ((start < len(old) && !utf8.RuneStart(old[start])) ||
		(start < len(new) && !utf8.RuneStart(new[start]))) {
		start--
	}
	if start == len(old) && start == len(new) {
		return start, start, start
	}
	// Common suffix, not overlapping the prefix.
	oldEnd, newEnd = len(old), len(new)
	for oldEnd > start && newEnd > start && old[oldEnd-1] == new[newEnd-1] {
		oldEnd--
		newEnd--
	}
	for oldEnd < len(old) && !utf8.RuneStart(old[oldEnd]) {
		oldEnd++
		newEnd++
	}
	return start, oldEnd, newEnd
}
