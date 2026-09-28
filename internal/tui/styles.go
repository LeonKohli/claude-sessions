package tui

import (
	"charm.land/lipgloss/v2"
	"image/color"
)

func newTheme(dark bool) theme {
	ld := lipgloss.LightDark(dark)
	var (
		colorPrimary = ld(lipgloss.Color("#005EA8"), lipgloss.Color("#79C0FF"))
		colorMuted   = ld(lipgloss.Color("#566675"), lipgloss.Color("#8FA3B5"))
		colorText    = ld(lipgloss.Color("#172B3A"), lipgloss.Color("#E6EDF3"))
		colorDimText = ld(lipgloss.Color("#435B6C"), lipgloss.Color("#B3C2CE"))
		colorBorder  = ld(lipgloss.Color("#C4D1DA"), lipgloss.Color("#334452"))
		colorBgPanel = ld(lipgloss.Color("#D7EBFC"), lipgloss.Color("#203D56"))
		colorSuccess = ld(lipgloss.Color("#166345"), lipgloss.Color("#7AD9A9"))
		colorWarning = ld(lipgloss.Color("#875B00"), lipgloss.Color("#EFC477"))
		colorClaude  = ld(lipgloss.Color("#8B431A"), lipgloss.Color("#F3BC97"))
		colorCodex   = ld(lipgloss.Color("#005785"), lipgloss.Color("#89D1FA"))

		panelStyle          = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
		activePanelStyle    = panelStyle.BorderForeground(colorPrimary)
		searchBarStyle      = panelStyle
		searchLabelStyle    = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
		searchModeStyle     = lipgloss.NewStyle().Foreground(colorPrimary).Background(colorBgPanel).Bold(true)
		itemTitleStyle      = lipgloss.NewStyle().Foreground(colorText).Bold(true)
		itemDateStyle       = lipgloss.NewStyle().Foreground(colorMuted)
		previewHeaderStyle  = lipgloss.NewStyle().Foreground(colorText).Bold(true)
		previewValueStyle   = lipgloss.NewStyle().Foreground(colorText)
		previewDividerStyle = lipgloss.NewStyle().Foreground(colorBorder)
		previewRoleUser     = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
		previewMsgText      = lipgloss.NewStyle().Foreground(colorDimText)
		statusBarStyle      = lipgloss.NewStyle().Padding(0, 1).Foreground(colorMuted)
		statusKeyStyle      = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
		statusDescStyle     = lipgloss.NewStyle().Foreground(colorMuted)
		filterActiveStyle   = lipgloss.NewStyle().Foreground(colorWarning)
		badgeClaudeStyle    = lipgloss.NewStyle().Foreground(colorClaude).
					Background(ld(lipgloss.Color("#FBE5D5"), lipgloss.Color("#49362C"))).Bold(true)
		badgeCodexStyle = lipgloss.NewStyle().Foreground(colorCodex).
				Background(ld(lipgloss.Color("#D5EEFC"), lipgloss.Color("#193D53"))).Bold(true)
	)

	return theme{
		colorPrimary:        colorPrimary,
		colorMuted:          colorMuted,
		colorText:           colorText,
		colorDimText:        colorDimText,
		colorBorder:         colorBorder,
		colorBgPanel:        colorBgPanel,
		colorSuccess:        colorSuccess,
		colorWarning:        colorWarning,
		colorClaude:         colorClaude,
		colorCodex:          colorCodex,
		panelStyle:          panelStyle,
		activePanelStyle:    activePanelStyle,
		searchBarStyle:      searchBarStyle,
		searchLabelStyle:    searchLabelStyle,
		searchModeStyle:     searchModeStyle,
		itemTitleStyle:      itemTitleStyle,
		itemDateStyle:       itemDateStyle,
		previewHeaderStyle:  previewHeaderStyle,
		previewValueStyle:   previewValueStyle,
		previewDividerStyle: previewDividerStyle,
		previewRoleUser:     previewRoleUser,
		previewMsgText:      previewMsgText,
		statusBarStyle:      statusBarStyle,
		statusKeyStyle:      statusKeyStyle,
		statusDescStyle:     statusDescStyle,
		filterActiveStyle:   filterActiveStyle,
		badgeClaudeStyle:    badgeClaudeStyle,
		badgeCodexStyle:     badgeCodexStyle,
	}
}

type theme struct {
	colorPrimary        color.Color
	colorMuted          color.Color
	colorText           color.Color
	colorDimText        color.Color
	colorBorder         color.Color
	colorBgPanel        color.Color
	colorSuccess        color.Color
	colorWarning        color.Color
	colorClaude         color.Color
	colorCodex          color.Color
	panelStyle          lipgloss.Style
	activePanelStyle    lipgloss.Style
	searchBarStyle      lipgloss.Style
	searchLabelStyle    lipgloss.Style
	searchModeStyle     lipgloss.Style
	itemTitleStyle      lipgloss.Style
	itemDateStyle       lipgloss.Style
	previewHeaderStyle  lipgloss.Style
	previewValueStyle   lipgloss.Style
	previewDividerStyle lipgloss.Style
	previewRoleUser     lipgloss.Style
	previewMsgText      lipgloss.Style
	statusBarStyle      lipgloss.Style
	statusKeyStyle      lipgloss.Style
	statusDescStyle     lipgloss.Style
	filterActiveStyle   lipgloss.Style
	badgeClaudeStyle    lipgloss.Style
	badgeCodexStyle     lipgloss.Style
}
