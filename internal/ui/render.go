package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
)

const renderDelay = 150 * time.Millisecond

// renderTickMsg fires after the debounce delay; stale seqs are ignored.
type renderTickMsg struct{ seq int }

func scheduleRender(seq int) tea.Cmd {
	return tea.Tick(renderDelay, func(time.Time) tea.Msg { return renderTickMsg{seq} })
}

// renderer caches a glamour renderer for a given width and theme, since
// building one is far more expensive than rendering with it.
type renderer struct {
	width int
	dark  bool
	tr    *glamour.TermRenderer
}

func (r *renderer) render(md string, width int, dark bool) string {
	width = max(width, 10)
	if r.tr == nil || r.width != width || r.dark != dark {
		style := "light"
		if dark {
			style = "dark"
		}
		tr, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			// glamour's document style adds a 2-col margin on each side.
			glamour.WithWordWrap(max(width-4, 6)),
			glamour.WithEmoji(),
		)
		if err != nil {
			return md
		}
		r.tr, r.width, r.dark = tr, width, dark
	}
	if strings.TrimSpace(md) == "" {
		return "\n  (empty note)"
	}
	out, err := r.tr.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimRight(out, "\n")
}
