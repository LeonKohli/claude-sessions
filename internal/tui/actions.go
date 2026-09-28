package tui

import (
	"fmt"
	"path/filepath"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/charmbracelet/x/ansi"
)

type actionItem struct {
	title, description, shortcut string
	run                          func(Model) (tea.Model, tea.Cmd)
}

func (a actionItem) Title() string {
	if a.shortcut != "" {
		return a.title + "  [" + a.shortcut + "]"
	}
	return a.title
}
func (a actionItem) Description() string { return a.description }
func (a actionItem) FilterValue() string { return a.title + " " + a.description }

type actionMenu struct {
	title string
	input textinput.Model
	list  list.Model
}

func (m *Model) openMenu(title string, actions []actionItem) tea.Cmd {
	items := make([]list.Item, len(actions))
	for i := range actions {
		items[i] = actions[i]
	}
	input := textinput.New()
	input.Prompt = "Find: "
	input.Placeholder = "Type an action or choice…"
	input.SetVirtualCursor(true)
	input.SetStyles(textinput.DefaultStyles(m.dark))
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	m.menu = &actionMenu{title: title, input: input, list: l}
	m.sizeMenu()
	return m.menu.input.Focus()
}

func (m *Model) sizeMenu() {
	if m.menu == nil {
		return
	}
	w, h := m.menuDimensions()
	m.menu.input.SetWidth(max(1, w-11))
	m.menu.list.SetSize(max(1, w-4), max(1, h-7))
	m.styleMenu()
}

func (m *Model) styleMenu() {
	if m.menu == nil {
		return
	}
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	d.Styles = list.NewDefaultItemStyles(m.dark)
	d.Styles.NormalTitle = m.theme.itemTitleStyle.PaddingLeft(2)
	d.Styles.NormalDesc = m.theme.statusDescStyle.PaddingLeft(2)
	d.Styles.SelectedTitle = d.Styles.NormalTitle.Background(m.theme.colorBgPanel).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(m.theme.colorPrimary).PaddingLeft(1).Width(m.menu.list.Width())
	d.Styles.SelectedDesc = d.Styles.SelectedTitle.Foreground(m.theme.colorDimText).Bold(false)
	m.menu.list.SetDelegate(d)
	m.menu.list.Styles = list.DefaultStyles(m.dark)
	m.menu.input.SetStyles(textinput.DefaultStyles(m.dark))
}

func (m Model) handleMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	menu := m.menu
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc", "ctrl+k", "f1":
			m.menu = nil
			return m, nil
		case "enter":
			if item, ok := menu.list.SelectedItem().(actionItem); ok {
				m.menu = nil
				return item.run(m)
			}
			return m, nil
		case "up", "down", "pgup", "pgdown", "ctrl+n", "ctrl+p":
			if k.String() == "ctrl+n" {
				msg = tea.KeyPressMsg{Code: tea.KeyDown}
			}
			if k.String() == "ctrl+p" {
				msg = tea.KeyPressMsg{Code: tea.KeyUp}
			}
			var cmd tea.Cmd
			menu.list, cmd = menu.list.Update(msg)
			return m, cmd
		}
	}
	old := menu.input.Value()
	var cmd tea.Cmd
	menu.input, cmd = menu.input.Update(msg)
	if menu.input.Value() != old {
		menu.list.SetFilterText(menu.input.Value())
		m.sizeMenu()
	}
	return m, cmd
}

func (m Model) renderMenu(base string) string {
	menu := m.menu
	w, h := m.menuDimensions()
	context := "Sessions"
	if len(m.filtered) > 0 {
		context = m.filtered[m.cursor].DisplayTitle()
	}
	body := menu.list.View()
	if len(menu.list.VisibleItems()) == 0 {
		body = m.theme.statusDescStyle.Render("No matching actions. Change the text or press Esc.")
	}
	footer := fmt.Sprintf("↑↓ choose · Enter apply · Esc cancel    %d choices", len(menu.list.VisibleItems()))
	content := lipgloss.JoinVertical(lipgloss.Left, m.theme.searchLabelStyle.Render(menu.title), m.theme.statusDescStyle.Render(clipLine(context, w-4)), menu.input.View(), "", lipgloss.NewStyle().Height(max(1, h-7)).Render(body), m.theme.statusDescStyle.Render(clipLine(footer, w-4)))
	bg := lipgloss.LightDark(m.dark)(lipgloss.Color("#FFFFFF"), lipgloss.Color("#101419"))
	dialog := m.theme.activePanelStyle.Background(bg).Width(w).Height(h).Render(content)
	backdrop := m.theme.statusDescStyle.Render(ansi.Strip(base))
	return lipgloss.NewCompositor(lipgloss.NewLayer(backdrop), lipgloss.NewLayer(dialog).X((m.width-w)/2).Y((m.height-h)/2)).Render()
}

func normalAction(title, description, shortcut string, msg tea.KeyPressMsg) actionItem {
	return actionItem{title, description, shortcut, func(m Model) (tea.Model, tea.Cmd) { return m.handleNormalKey(msg) }}
}

func (m *Model) openActions() tea.Cmd {
	var actions []actionItem
	add := func(title, description, shortcut string, msg tea.KeyPressMsg) {
		actions = append(actions, normalAction(title, description, shortcut, msg))
	}
	switch {
	case m.files != nil:
		back := "Back to sessions"
		if m.reader != nil {
			back = "Back to conversation"
		}
		actions = append(actions, actionItem{"Switch file/content focus", "Navigate filenames or scroll recorded content", "Tab", func(m Model) (tea.Model, tea.Cmd) { return m.handleFileKey(tea.KeyPressMsg{Code: tea.KeyTab}) }}, actionItem{back, "Keep the selected session and reader position", "Esc", func(m Model) (tea.Model, tea.Cmd) { m.files = nil; return m, nil }})
	case m.reader != nil:
		actions = append(actions, actionItem{"Find in conversation", "Highlight text without hiding surrounding messages", "/", func(m Model) (tea.Model, tea.Cmd) { m.reader.finding = true; return m, m.reader.find.Focus() }})
		actions = append(actions, actionItem{"Toggle Markdown/source", "Read formatted answers or inspect their original text", "m", func(m Model) (tea.Model, tea.Cmd) { cmd := m.toggleReaderSource(); return m, cmd }})
		if m.reader.matches > 0 {
			actions = append(actions, actionItem{"Next match", "Jump to the next search occurrence", "n", func(m Model) (tea.Model, tea.Cmd) { m.reader.viewport.HighlightNext(); return m, nil }}, actionItem{"Previous match", "Jump to the previous search occurrence", "N", func(m Model) (tea.Model, tea.Cmd) { m.reader.viewport.HighlightPrevious(); return m, nil }})
		}
		actions = append(actions, actionItem{"Back to sessions", "Keep the search and selected result", "Esc", func(m Model) (tea.Model, tea.Cmd) { m.reader = nil; return m, nil }})
	default:
		if len(m.filtered) > 0 {
			add("Read conversation", "Open the selected session without starting an agent", "Enter", tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		add("Search sessions", "Search titles and metadata, or transcript contents", "/", tea.KeyPressMsg{Text: "/"})
		add("Choose project", "Select a project by name or path", "p", tea.KeyPressMsg{Text: "p"})
		add("Choose agent", "Show Claude, Codex, or both", "f", tea.KeyPressMsg{Text: "f"})
		add("Choose date range", "Limit sessions by last modified time", "d", tea.KeyPressMsg{Text: "d"})
		add("Choose sort order", "Sort sessions by time, usage, or size", "s", tea.KeyPressMsg{Text: "s"})
		add("Toggle transcript search", "Switch between metadata and transcript contents", "Ctrl+s", tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		subagents := "Show subagent sessions"
		if m.showSubagents {
			subagents = "Hide subagent sessions"
		}
		add(subagents, "Include or exclude spawned agent threads", "a", tea.KeyPressMsg{Text: "a"})
		add("Switch list/preview focus", "Move within the list or scroll the preview", "Tab", tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if m.files == nil && len(m.filtered) > 0 {
		add("Open recorded files", "Inspect saved historical content and recorded diffs", "o", tea.KeyPressMsg{Text: "o"})
		add("Copy session reference", "Copy the archive identifier to the clipboard", "y", tea.KeyPressMsg{Text: "y"})
		s := m.filtered[m.cursor]
		if s.Provider != provider.Claude || !s.IsSubagent {
			add("Resume session", "Leave this browser and launch the owning agent", "r", tea.KeyPressMsg{Text: "r"})
		}
	}
	add("Quit", "Close the session browser", "q", tea.KeyPressMsg{Text: "q"})
	title := "Session actions"
	if m.reader != nil {
		title = "Conversation actions"
	}
	if m.files != nil {
		title = "Recorded file actions"
	}
	if m.editingText() {
		for i := range actions {
			actions[i].shortcut = ""
		}
	}
	return m.openMenu(title, actions)
}

func (m *Model) openProjectMenu() tea.Cmd {
	actions := []actionItem{{"All projects", "Clear the project filter", "", func(m Model) (tea.Model, tea.Cmd) { m.projectFilter = ""; return m, m.refilter() }}}
	for _, path := range m.projects {
		title := terminalText(filepath.Base(path))
		if path == m.projectFilter {
			title += " · current"
		}
		actions = append(actions, actionItem{title, terminalText(path), "", func(m Model) (tea.Model, tea.Cmd) { m.projectFilter = path; return m, m.refilter() }})
	}
	return m.openMenu("Choose project", actions)
}

func (m *Model) openProviderMenu() tea.Cmd {
	var actions []actionItem
	for _, p := range []providerFilter{providerAll, providerClaude, providerCodex} {
		title := p.String()
		if p == m.providerFilter {
			title += " · current"
		}
		actions = append(actions, actionItem{title, "Filter sessions by the owning agent", "", func(m Model) (tea.Model, tea.Cmd) { m.providerFilter = p; return m, m.refilter() }})
	}
	return m.openMenu("Choose agent", actions)
}

func (m *Model) openDateMenu() tea.Cmd {
	var actions []actionItem
	for _, d := range []dateFilter{dateAll, dateToday, dateWeek, dateMonth} {
		title := d.String()
		if d == m.dateFilter {
			title += " · current"
		}
		actions = append(actions, actionItem{title, "Filter by session modification time", "", func(m Model) (tea.Model, tea.Cmd) { m.dateFilter = d; return m, m.refilter() }})
	}
	return m.openMenu("Choose date range", actions)
}

func (m *Model) openSortMenu() tea.Cmd {
	labels := []string{"Recently modified", "Recently created", "Most tokens", "Most messages", "Longest duration", "Largest transcript"}
	var actions []actionItem
	for i, title := range labels {
		mode := sortMode(i)
		if mode == m.sortBy {
			title += " · current"
		}
		actions = append(actions, actionItem{title, "Highest or newest first", "", func(m Model) (tea.Model, tea.Cmd) { m.sortBy = mode; return m, m.refilter() }})
	}
	return m.openMenu("Choose sort order", actions)
}

func (m *Model) refilter() tea.Cmd {
	m.cursor, m.scrollOffset = 0, 0
	m.applyFilters()
	return m.triggerPreview()
}

func (m Model) editingText() bool {
	if m.files != nil {
		return false
	}
	if m.reader != nil {
		return m.reader.finding
	}
	return m.searchActive
}

func (m Model) menuDimensions() (int, int) {
	return min(78, m.width-2), min(24, m.height-2, 7+2*max(1, len(m.menu.list.VisibleItems())))
}
