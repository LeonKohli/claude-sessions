package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Scope      key.Binding
	Resume     key.Binding
	Files      key.Binding
	Quit       key.Binding
	Up         key.Binding
	Down       key.Binding
	Enter      key.Binding
	Tab        key.Binding
	Search     key.Binding
	Escape     key.Binding
	CopyUUID   key.Binding
	Project    key.Binding
	DateFilter key.Binding
	Provider   key.Binding
	Subagents  key.Binding
	Sort       key.Binding
	Help       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	HalfUp     key.Binding
	HalfDown   key.Binding
	GotoTop    key.Binding
	GotoBottom key.Binding
}

var keys = keyMap{
	Scope:  key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "search scope")),
	Resume: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "resume")),
	Files:  key.NewBinding(key.WithKeys("o"), key.WithHelp("[o]", "Open files")),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "read"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "focus"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "clear/back"),
	),
	CopyUUID: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "copy ID"),
	),
	Project: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "choose project"),
	),
	DateFilter: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "choose date range"),
	),
	Provider: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "choose agent"),
	),
	Subagents: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "toggle subagent threads"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "choose sort"),
	),
	Help: key.NewBinding(
		key.WithKeys("?", "ctrl+k", "f1"),
		key.WithHelp("ctrl+k", "Actions"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "page down"),
	),
	HalfUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("C-u", "half page up"),
	),
	HalfDown: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("C-d", "half page down"),
	),
	GotoTop: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "go to top"),
	),
	GotoBottom: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "go to bottom"),
	),
}
