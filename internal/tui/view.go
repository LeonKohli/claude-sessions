package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/LeonKohli/claude-sessions/internal/util"
)

func (m Model) View() tea.View {
	content := m.render()
	if m.menu != nil && m.width >= 40 && m.height >= 18 {
		content = m.renderMenu(content)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	if m.width < 40 || m.height < 18 {
		return "Enlarge terminal to at least 40 × 18"
	}
	if m.files != nil {
		return m.renderFiles()
	}
	if m.reader != nil {
		return m.renderReader()
	}
	searchBar := m.renderSearchBar()
	statusBar := m.renderStatusBar()

	leftW, rightW, listH, previewH := m.panelDimensions()

	if m.width < 90 {
		return lipgloss.JoinVertical(lipgloss.Left, searchBar, m.renderList(leftW, listH), m.renderPreview(rightW, previewH), statusBar)
	}
	leftPanel := m.renderList(leftW, listH)
	rightPanel := m.renderPreview(rightW, previewH)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	return lipgloss.JoinVertical(lipgloss.Left, searchBar, panels, statusBar)
}

func (m Model) panelDimensions() (listW, previewW, listH, previewH int) {
	h := m.height - lipgloss.Height(m.renderSearchBar()) - lipgloss.Height(m.renderStatusBar())
	if m.width < 90 {
		return m.width, m.width, 7, h - 7
	}
	listW = m.width * 44 / 100
	return listW, m.width - listW, h, h
}

func (m Model) renderSearchBar() string {
	count := strconv.Itoa(len(m.filtered)) + " / " + strconv.Itoa(len(m.allSessions)) + " sessions"
	if m.deepSearching {
		count = "Searching transcripts…"
	}
	if m.toast != "" {
		count = m.toast
	}
	heading := m.theme.searchLabelStyle.Render("agent-sessions")
	count = clipLine(count, m.width-3-lipgloss.Width(heading))
	top := heading + strings.Repeat(" ", max(1, m.width-2-lipgloss.Width(heading)-lipgloss.Width(count))) + m.theme.statusDescStyle.Render(count)
	mode := m.theme.statusDescStyle.Render("titles") + "  " + m.theme.searchModeStyle.Render("[transcript]")
	if m.searchMode == searchFuzzy {
		mode = m.theme.searchModeStyle.Render("[titles]") + "  " + m.theme.statusDescStyle.Render("transcript")
	}
	input := m.searchInput
	input.SetWidth(m.searchFieldWidth())
	row := m.theme.searchLabelStyle.Render("Search: ") + lipgloss.NewStyle().Width(input.Width()+1).Render(input.View()) + "  " + mode
	style := m.theme.searchBarStyle
	if m.searchActive {
		style = m.theme.activePanelStyle
	}
	filters := []string{"sort: " + m.sortBy.String()}
	if m.projectFilter != "" {
		filters = append(filters, "project: "+filepath.Base(m.projectFilter))
	}
	if m.providerFilter != providerAll {
		filters = append(filters, "agent: "+m.providerFilter.String())
	}
	if m.dateFilter != dateAll {
		filters = append(filters, "date: "+m.dateFilter.String())
	}
	if m.showSubagents {
		filters = append(filters, "subagents shown")
	}
	return lipgloss.JoinVertical(lipgloss.Left, " "+top, style.Border(lipgloss.Border{}).Padding(0, 1).Width(m.width).Render(row), " "+m.theme.statusDescStyle.Render(clipLine(strings.Join(filters, "   "), m.width-2)))
}

func (m Model) searchFieldWidth() int { return max(1, m.width-39) }

func (m Model) renderList(w, h int) string {
	if h < 4 {
		h = 4
	}

	itemH, headerH := m.listItemHeight(), 1
	if m.width < 90 {
		headerH = 1
	}
	visibleItems := max(1, (h-2-headerH)/itemH)

	if len(m.filtered) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(m.theme.colorMuted).
			Padding(1, 1).
			Render("No sessions found")
		return m.theme.panelStyle.Width(w).Height(h).Render(empty)
	}

	end := m.scrollOffset + visibleItems
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	heading := m.theme.statusDescStyle.Render("Sessions")
	if m.width >= 90 {
		heading = "Sessions · Enter read"
		if !m.searchActive && !m.previewFocused {
			heading = "Sessions · focused"
		}
	}
	lines := []string{heading}
	for i := m.scrollOffset; i < end; i++ {
		s := m.filtered[i]
		selected := i == m.cursor
		innerW := max(1, w-4)
		badge := m.providerBadge(s.Provider)
		titleW := max(1, innerW-lipgloss.Width(badge)-3)
		marker := "  "
		titleStyle, detailStyle := m.theme.itemTitleStyle, m.theme.itemDateStyle
		if selected {
			marker = "▸ "
			titleStyle = titleStyle.Background(m.theme.colorBgPanel)
			detailStyle = detailStyle.Background(m.theme.colorBgPanel)
		}
		title := titleStyle.Width(titleW).Render(clipLine(s.DisplayTitle(), titleW))
		project := shortPath(s.ProjectPath, innerW-15)
		if s.IsSubagent {
			label := s.AgentLabel
			if label == "" {
				label = "subagent"
			}
			project = label + " · " + project
		}
		age := util.RelativeTime(s.Modified)
		detail := clipLine(project, innerW-lipgloss.Width(age)-4)
		detail = "  " + detail + strings.Repeat(" ", max(1, innerW-2-lipgloss.Width(detail)-lipgloss.Width(age))) + age
		lines = append(lines, titleStyle.Render(marker)+badge+" "+title, detailStyle.Width(innerW).Render(detail))
		if itemH == 3 {
			snippet := ""
			if hits := m.deepResults[s.ReferenceID()]; len(hits) > 0 {
				snippet = hits[0]
			}
			lines = append(lines, m.theme.filterActiveStyle.Render(clipLine("  ↳ "+snippet, innerW)))
		}
	}

	content := strings.Join(lines, "\n")

	style := m.theme.panelStyle
	if !m.searchActive && !m.previewFocused {
		style = m.theme.activePanelStyle
	}

	return style.Width(w).Height(h).Render(content)
}

func (m Model) previewContent(w, h int) string {
	if h < 4 {
		h = 4
	}

	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		empty := lipgloss.NewStyle().
			Foreground(m.theme.colorMuted).
			Padding(1, 1).
			Render("No session selected")
		return m.theme.panelStyle.Width(w).Height(h).Render(empty)
	}

	s := m.filtered[m.cursor]
	innerW := max(1, w-4)
	var lines []string

	lines = append(lines, m.providerBadge(s.Provider)+"  "+m.theme.statusDescStyle.Render(clipLine(s.ProjectPath, innerW-11)))
	title := terminalText(s.DisplayTitle())
	if m.width < 90 {
		title = clipLine(title, innerW)
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, m.theme.previewHeaderStyle.Width(innerW).Render(title))
	var stats []string
	if count := s.EstimatedMessages(); count > 0 || s.Enriched {
		label := strconv.Itoa(count) + " messages"
		if s.MessageCount == 0 && !s.Enriched {
			label = "~" + label
		}
		stats = append(stats, label)
	}
	if s.TotalTokens() > 0 {
		stats = append(stats, util.FormatTokens(s.TotalTokens())+"t")
	}
	if s.Model != "" {
		stats = append(stats, s.Model)
	}
	lines = append(lines, m.theme.statusDescStyle.Render(clipLine(strings.Join(stats, "  ·  "), innerW)))
	lines = append(lines, m.theme.previewValueStyle.Render(m.fileSummary(s.Provider)))
	context := s.GitBranch
	if context != "" {
		context += "  ·  "
	}
	context += s.Modified.Format("02 Jan 15:04") + "  ·  " + displayID(s)
	if m.width >= 90 {
		lines = append(lines, m.theme.statusDescStyle.Render(clipLine(context, innerW)))
	}
	if s.IsSubagent {
		label := s.AgentLabel
		if label == "" {
			label = "subagent"
		}
		if s.Provider == provider.Claude {
			label += " (not resumable)"
		}
		lines = append(lines, m.theme.filterActiveStyle.Render(terminalText(label)))
	}
	if s.Archived {
		lines = append(lines, m.theme.filterActiveStyle.Render("Archived session"))
	}
	if m.width >= 90 {
		lines = append(lines, "")
	}
	fileHint := m.theme.statusKeyStyle.Render("[o]") + m.theme.statusDescStyle.Render(" Open files")
	lines = append(lines, m.theme.itemTitleStyle.Render("Conversation")+strings.Repeat(" ", max(1, innerW-12-lipgloss.Width(fileHint)))+fileHint, m.theme.previewDividerStyle.Render(strings.Repeat("─", innerW)))
	// Deep search snippets
	if m.deepResults != nil {
		if snippets, ok := m.deepResults[s.ReferenceID()]; ok && len(snippets) > 0 {
			lines = append(lines, "")
			lines = append(lines, m.theme.previewDividerStyle.Render(strings.Repeat("─", innerW)))
			lines = append(lines, m.theme.filterActiveStyle.Render(" Matches:"))
			for i, snip := range snippets {
				if i >= 5 {
					remaining := strconv.Itoa(len(snippets) - 5)
					lines = append(lines, m.theme.statusDescStyle.Render("  ... +"+remaining+" more"))
					break
				}
				lines = append(lines, m.theme.previewMsgText.Render("  "+clipLine(snip, innerW-4)))
			}
		}
	}

	// Conversation preview
	if len(m.previewMsgs) > 0 && m.previewSessID == s.ReferenceID() {

		lines = append(lines, m.previewText, "")
	}
	if m.previewErr != nil {
		lines = append(lines, "", "Preview unavailable: "+terminalText(m.previewErr.Error()))
	} else if m.previewLoading {
		lines = append(lines, "")
		lines = append(lines, m.theme.statusDescStyle.Render(" Loading..."))
	}

	lines = append(lines, "", m.theme.itemTitleStyle.Render("Session details"), m.theme.previewDividerStyle.Render(strings.Repeat("─", innerW)))
	details := []string{"Reference: " + s.ReferenceID(), "Project: " + s.ProjectPath, "Created: " + s.Created.Format("2006-01-02 15:04"), "Transcript: " + util.FormatSize(s.FileSize)}
	if s.GitBranch != "" {
		details = append(details, "Branch: "+s.GitBranch)
	}
	if duration := s.Duration(); duration > 0 {
		details = append(details, "Duration: "+util.FormatDuration(duration))
	}
	if s.TotalTokens() > 0 {
		details = append(details, "Tokens: in "+util.FormatTokens(s.TotalInputTokens)+" / out "+util.FormatTokens(s.TotalOutputTokens)+" / cache read "+util.FormatTokens(s.CacheReadTokens)+" / cache write "+util.FormatTokens(s.CacheWriteTokens))
	}
	if len(s.ToolsUsed) > 0 {
		details = append(details, "Tools: "+strings.Join(s.ToolsUsed, ", "))
	}
	for _, detail := range details {
		lines = append(lines, m.theme.statusDescStyle.Render(terminalText(detail)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderPreview(w, h int) string {
	style := m.theme.panelStyle
	if m.previewFocused {
		style = m.theme.activePanelStyle
	}
	hintText := "Tab focus · Enter read"
	if m.previewFocused {
		hintText = "Preview focused · ↑↓ scroll · g/G first/last"
	}
	hint := m.theme.statusDescStyle.Render(clipLine(hintText, w-4))
	return style.Width(w).Height(h).MaxHeight(h).Render(m.preview.View() + "\n" + hint)
}

// providerBadge renders the agent tag shown beside a session.
func (m Model) providerBadge(k provider.Kind) string {
	if k == provider.Codex {
		return m.theme.badgeCodexStyle.Render(" Codex  ")
	}
	return m.theme.badgeClaudeStyle.Render(" Claude ")
}

// shortPath makes a path displayable: ~/proj or ~/Documents/proj
func shortPath(p string, maxW int) string {
	if p == "" {
		return "—"
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	if len(p) <= maxW {
		return p
	}
	parts := strings.Split(p, "/")
	if len(parts) >= 2 {
		short := "…/" + strings.Join(parts[len(parts)-2:], "/")
		if len(short) <= maxW {
			return short
		}
	}
	return util.Truncate(p, maxW)
}

func (m Model) renderStatusBar() string {
	h := help.New()
	h.SetWidth(m.width - 2)
	h.ShortSeparator = "   "
	h.Styles.ShortKey = m.theme.statusKeyStyle
	h.Styles.ShortDesc = m.theme.statusDescStyle
	bindings := []key.Binding{keys.Help, keys.Enter, keys.Search, keys.Tab, keys.Files, keys.Quit}
	if m.searchActive {
		accept, back := keys.Enter, keys.Escape
		accept.SetHelp("enter", "read")
		back.SetHelp("esc", "back")
		bindings = []key.Binding{keys.Help, keys.Scope, accept, back}
	}
	return m.theme.statusBarStyle.Render(ansi.Truncate(h.ShortHelpView(bindings), max(1, m.width-2), "…"))
}

func displayID(s session.SessionEntry) string {
	ref := s.ReferenceID()
	if i := strings.LastIndexByte(ref, '/'); i >= 0 {
		ref = ref[i+1:]
	}
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

func (m Model) fileSummary(kind provider.Kind) string {
	if m.previewLoading {
		return "Recorded files: loading…"
	}
	if m.previewFilesErr != nil {
		return "Recorded files: unavailable"
	}
	saved, added, updated, deleted := 0, 0, 0, 0
	for _, f := range m.previewFiles {
		if f.Recoverable {
			saved++
		}
		switch f.Kind {
		case session.ChangeAdd:
			added++
		case session.ChangeUpdate:
			updated++
		case session.ChangeDelete:
			deleted++
		}
	}
	summary := fmt.Sprintf("Recorded files: %d · %d with saved content", len(m.previewFiles), saved)
	if len(m.previewFiles) > 0 {
		summary += fmt.Sprintf("\n%d added · %d updated · %d deleted", added, updated, deleted)
		if kind == provider.Claude {
			summary += " (inferred)"
		}
	}
	return summary
}
