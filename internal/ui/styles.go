package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type styles struct {
	bar       lipgloss.Style
	title     lipgloss.Style
	dirty     lipgloss.Style
	badge     map[mode]lipgloss.Style
	status    lipgloss.Style
	statusMsg lipgloss.Style
	statusErr lipgloss.Style
	hint      lipgloss.Style
	divider   lipgloss.Style
	helpBox   lipgloss.Style

	menuBox      lipgloss.Style
	menuItem     lipgloss.Style
	menuMuted    lipgloss.Style
	menuSel      lipgloss.Style
	menuSelMuted lipgloss.Style
}

func newStyles(dark bool) styles {
	ld := lipgloss.LightDark(dark)
	barBG := ld(lipgloss.Color("#E8E6F0"), lipgloss.Color("#1E1B2E"))
	fg := ld(lipgloss.Color("#2B2540"), lipgloss.Color("#DCD7F0"))
	muted := ld(lipgloss.Color("#77708F"), lipgloss.Color("#8A84A3"))
	accent := lipgloss.Color("#7C5CFF") // Obsidian-ish violet
	menuBG := ld(lipgloss.Color("#F6F5FA"), lipgloss.Color("#262338"))

	badge := func(bg color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.Color("#FFFFFF")).Background(bg)
	}
	bar := lipgloss.NewStyle().Background(barBG).Foreground(fg)
	return styles{
		bar:   bar,
		title: bar.Bold(true).Padding(0, 1),
		dirty: bar.Foreground(lipgloss.Color("#E5A50A")),
		badge: map[mode]lipgloss.Style{
			modeEdit:  badge(accent),
			modeRead:  badge(lipgloss.Color("#2E9E6B")),
			modeSplit: badge(lipgloss.Color("#3D7FD9")),
		},
		status:    bar.Foreground(muted).Padding(0, 1),
		statusMsg: bar.Foreground(lipgloss.Color("#2E9E6B")).Bold(true).Padding(0, 1),
		statusErr: bar.Foreground(lipgloss.Color("#E5484D")).Bold(true).Padding(0, 1),
		hint:      bar.Foreground(muted).Padding(0, 1),
		divider:   lipgloss.NewStyle().Foreground(muted),
		helpBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).Padding(1, 2),

		menuBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).BorderBackground(menuBG).Background(menuBG),
		menuItem:     lipgloss.NewStyle().Background(menuBG).Foreground(fg),
		menuMuted:    lipgloss.NewStyle().Background(menuBG).Foreground(muted),
		menuSel:      lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		menuSelMuted: lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#E4DCFF")),
	}
}
