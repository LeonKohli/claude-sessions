package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary   = lipgloss.Color("#7C3AED") // purple
	colorSecondary = lipgloss.Color("#A78BFA") // light purple
	colorMuted     = lipgloss.Color("#6B7280") // gray
	colorSuccess   = lipgloss.Color("#10B981") // green
	colorWarning   = lipgloss.Color("#F59E0B") // amber
	colorDanger    = lipgloss.Color("#EF4444") // red
	colorBg        = lipgloss.Color("#1A1A2E") // dark bg
	colorBgPanel   = lipgloss.Color("#16213E") // panel bg
	colorBorder    = lipgloss.Color("#374151") // border
	colorHighlight = lipgloss.Color("#7C3AED") // highlight border
	colorText      = lipgloss.Color("#E5E7EB") // main text
	colorDimText   = lipgloss.Color("#9CA3AF") // dim text
	colorClaude    = lipgloss.Color("#D97757") // Claude terracotta
	colorCodex     = lipgloss.Color("#10A37F") // OpenAI green

	// Panels
	panelStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	activePanelStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(colorHighlight).
				Padding(0, 1)

	// Search bar
	searchBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1).
			MarginBottom(0)

	searchLabelStyle = lipgloss.NewStyle().
				Foreground(colorSecondary).
				Bold(true)

	searchModeStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1).
			Background(lipgloss.Color("#2D1B69"))

	// List items
	itemTitleStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	itemSelectedStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	itemDateStyle = lipgloss.NewStyle().
			Foreground(colorDimText)

	itemProjectStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	itemPromptStyle = lipgloss.NewStyle().
			Foreground(colorDimText).
			Italic(true)

	// Right-aligned metadata in list items
	itemMetaStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	itemMetaDimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4B5563")) // darker gray for unselected

	// Preview
	previewHeaderStyle = lipgloss.NewStyle().
				Foreground(colorSecondary).
				Bold(true).
				MarginBottom(1)

	previewLabelStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	previewValueStyle = lipgloss.NewStyle().
				Foreground(colorText)

	previewDividerStyle = lipgloss.NewStyle().
				Foreground(colorBorder)

	previewRoleUser = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	previewRoleAssistant = lipgloss.NewStyle().
				Foreground(colorSecondary).
				Bold(true)

	previewMsgText = lipgloss.NewStyle().
			Foreground(colorDimText)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1).
			Foreground(colorDimText)

	statusKeyStyle = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	statusDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Filters
	filterActiveStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	filterLabelStyle = lipgloss.NewStyle().
				Foreground(colorDimText)

	// Toast / notification
	toastStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	// Provider badges
	badgeClaudeStyle = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorClaude).
				Bold(true)

	badgeCodexStyle = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorCodex).
			Bold(true)
)
