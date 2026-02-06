package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/leon/claude-sessions/internal/session"
	"github.com/leon/claude-sessions/internal/util"
)

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	searchBar := m.renderSearchBar()
	statusBar := m.renderStatusBar()

	searchH := lipgloss.Height(searchBar)
	statusH := lipgloss.Height(statusBar)
	panelH := m.height - searchH - statusH

	// 45/55 split — give preview more room for the rich data
	leftW := m.width*45/100 - 2
	rightW := m.width - leftW - 4

	leftPanel := m.renderList(leftW, panelH)
	rightPanel := m.renderPreview(rightW, panelH)

	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	return lipgloss.JoinVertical(lipgloss.Left, searchBar, panels, statusBar)
}

func (m Model) renderSearchBar() string {
	w := m.width - 4

	modeStr := " fuzzy "
	if m.searchMode == searchDeep {
		modeStr = " deep "
	}
	mode := searchModeStyle.Render(modeStr)

	var filterParts []string
	if m.projectFilter != "" {
		short := filepath.Base(m.projectFilter)
		filterParts = append(filterParts, filterActiveStyle.Render("P:"+short))
	}
	if m.dateFilter != dateAll {
		filterParts = append(filterParts, filterActiveStyle.Render("D:"+m.dateFilter.String()))
	}
	filterParts = append(filterParts, statusDescStyle.Render("S:")+filterActiveStyle.Render(m.sortBy.String()))
	filters := strings.Join(filterParts, " ")

	label := searchLabelStyle.Render("Search: ")
	searchField := m.searchInput.View()

	countStr := statusDescStyle.Render(
		" " + strconv.Itoa(len(m.filtered)) + "/" + strconv.Itoa(len(m.allSessions)))

	if m.deepSearching {
		countStr = filterActiveStyle.Render(" searching...")
	}

	if m.toast != "" {
		countStr = toastStyle.Render(" " + m.toast)
	}

	right := mode + " " + filters + countStr
	leftSpace := w - lipgloss.Width(label) - lipgloss.Width(right) - 2
	if leftSpace < 10 {
		leftSpace = 10
	}

	inputStyle := lipgloss.NewStyle().Width(leftSpace)
	row := label + inputStyle.Render(searchField) + " " + right

	return searchBarStyle.Width(m.width - 2).Render(row)
}

func (m Model) renderList(w, h int) string {
	if h < 4 {
		h = 4
	}

	itemH := 3
	visibleItems := (h - 2) / itemH

	if len(m.filtered) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(2, 2).
			Render("No sessions found")
		return panelStyle.Width(w).Height(h - 2).Render(empty)
	}

	end := m.scrollOffset + visibleItems
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	var lines []string
	for i := m.scrollOffset; i < end; i++ {
		s := m.filtered[i]
		selected := i == m.cursor
		innerW := w - 4 // border + padding

		// Line 1: relative time | short ID | model badge | msg count | duration
		relTime := util.RelativeTime(s.Modified)
		shortID := s.ShortID
		if shortID == "" && len(s.SessionID) >= 8 {
			shortID = s.SessionID[:8]
		}

		// Build right-side metadata
		var meta []string
		msgs := s.EstimatedMessages()
		if msgs > 0 {
			msgStr := strconv.Itoa(msgs) + "m"
			if s.MessageCount == 0 {
				msgStr = "~" + msgStr // estimated
			}
			meta = append(meta, msgStr)
		}
		dur := s.Duration()
		if dur > 0 {
			meta = append(meta, util.FormatDuration(dur))
		}
		if s.Enriched && s.TotalTokens() > 0 {
			meta = append(meta, util.FormatTokens(s.TotalTokens())+"t")
		} else if s.FileSize > 0 {
			meta = append(meta, util.FormatSize(s.FileSize))
		}
		if s.Model != "" {
			meta = append(meta, s.Model)
		}

		metaStr := strings.Join(meta, " │ ")

		// Line 2: project path
		project := shortPath(s.ProjectPath, innerW)

		// Line 3: title (summary or first prompt)
		title := s.DisplayTitle()
		title = util.Truncate(title, innerW-2)

		var line1, line2, line3 string
		if selected {
			// Selected: highlighted
			indicator := itemSelectedStyle.Render("▸ ")
			timeStr := itemSelectedStyle.Render(relTime)
			idStr := itemSelectedStyle.Render(shortID)
			metaRendered := itemMetaStyle.Render(metaStr)
			// Calculate padding between left and right
			leftPart := indicator + timeStr + " " + idStr
			leftW := lipgloss.Width(leftPart)
			metaW := lipgloss.Width(metaRendered)
			gap := innerW - leftW - metaW
			if gap < 1 {
				gap = 1
			}
			line1 = leftPart + strings.Repeat(" ", gap) + metaRendered
			line2 = "  " + itemProjectStyle.Render(project)
			line3 = "  " + itemPromptStyle.Render(title)
		} else {
			indicator := "  "
			timeStr := itemDateStyle.Render(relTime)
			idStr := itemTitleStyle.Render(shortID)
			metaRendered := itemMetaDimStyle.Render(metaStr)
			leftPart := indicator + timeStr + " " + idStr
			leftW := lipgloss.Width(leftPart)
			metaW := lipgloss.Width(metaRendered)
			gap := innerW - leftW - metaW
			if gap < 1 {
				gap = 1
			}
			line1 = leftPart + strings.Repeat(" ", gap) + metaRendered
			line2 = "  " + itemProjectStyle.Render(project)
			line3 = "  " + itemPromptStyle.Render(title)
		}

		lines = append(lines, line1, line2, line3)
	}

	content := strings.Join(lines, "\n")

	style := panelStyle
	if !m.searchActive {
		style = activePanelStyle
	}

	return style.Width(w).Height(h - 2).Render(content)
}

func (m Model) renderPreview(w, h int) string {
	if h < 4 {
		h = 4
	}

	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		empty := lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(2, 2).
			Render("No session selected")
		return panelStyle.Width(w).Height(h - 2).Render(empty)
	}

	s := m.filtered[m.cursor]
	innerW := w - 4
	var lines []string

	// Header: Session UUID
	lines = append(lines, previewHeaderStyle.Render(s.SessionID))

	// Metadata section
	addField := func(label, value string) {
		if value != "" {
			lines = append(lines, previewLabelStyle.Render(label)+previewValueStyle.Render(value))
		}
	}

	addField("Project:  ", s.ProjectPath)
	if s.GitBranch != "" {
		addField("Branch:   ", s.GitBranch)
	}

	// Time row: created → modified (duration)
	timeStr := s.Created.Format("2006-01-02 15:04")
	dur := s.Duration()
	if dur > 0 {
		timeStr += "  →  " + s.Modified.Format("15:04") + "  (" + util.FormatDuration(dur) + ")"
	}
	addField("Time:     ", timeStr)

	// Stats row: messages | size | model
	var stats []string
	msgs := s.EstimatedMessages()
	if msgs > 0 {
		msgLabel := strconv.Itoa(msgs) + " messages"
		if s.MessageCount == 0 {
			msgLabel = "~" + msgLabel
		}
		stats = append(stats, msgLabel)
	}
	stats = append(stats, util.FormatSize(s.FileSize))
	if s.Model != "" {
		stats = append(stats, s.Model)
	}
	addField("Stats:    ", strings.Join(stats, "  │  "))

	// Token stats (from enrichment)
	enrich := m.previewEnrichment
	if enrich == nil && s.Enriched {
		// Use cached enrichment from session entry
		enrich = &session.EnrichmentData{
			TotalInputTokens:  s.TotalInputTokens,
			TotalOutputTokens: s.TotalOutputTokens,
			CacheReadTokens:   s.CacheReadTokens,
			CacheWriteTokens:  s.CacheWriteTokens,
			Model:             s.Model,
			ToolsUsed:         s.ToolsUsed,
			FilesModified:     s.FilesModified,
		}
	}

	if enrich != nil && (enrich.TotalInputTokens > 0 || enrich.TotalOutputTokens > 0) {
		tokenStr := "in: " + util.FormatTokens(enrich.TotalInputTokens) +
			"  out: " + util.FormatTokens(enrich.TotalOutputTokens)
		if enrich.CacheReadTokens > 0 {
			tokenStr += "  cached: " + util.FormatTokens(enrich.CacheReadTokens)
		}
		addField("Tokens:   ", tokenStr)
	}

	// Tools used
	if enrich != nil && len(enrich.ToolsUsed) > 0 {
		toolStr := strings.Join(enrich.ToolsUsed, ", ")
		addField("Tools:    ", util.Truncate(toolStr, innerW-12))
	}

	// Files modified
	if enrich != nil && len(enrich.FilesModified) > 0 {
		fileCount := strconv.Itoa(len(enrich.FilesModified)) + " files"
		// Show first few filenames
		var fileNames []string
		for i, f := range enrich.FilesModified {
			if i >= 5 {
				fileNames = append(fileNames, "+"+strconv.Itoa(len(enrich.FilesModified)-5)+" more")
				break
			}
			fileNames = append(fileNames, filepath.Base(f))
		}
		addField("Files:    ", fileCount+" — "+strings.Join(fileNames, ", "))
	}

	// Deep search snippets
	if m.deepResults != nil {
		if snippets, ok := m.deepResults[s.SessionID]; ok && len(snippets) > 0 {
			lines = append(lines, "")
			lines = append(lines, previewDividerStyle.Render(strings.Repeat("─", innerW)))
			lines = append(lines, filterActiveStyle.Render(" Matches:"))
			for i, snip := range snippets {
				if i >= 5 {
					remaining := strconv.Itoa(len(snippets) - 5)
					lines = append(lines, statusDescStyle.Render("  ... +"+remaining+" more"))
					break
				}
				lines = append(lines, previewMsgText.Render("  "+util.Truncate(snip, innerW-4)))
			}
		}
	}

	// Summary
	if s.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, previewDividerStyle.Render(strings.Repeat("─", innerW)))
		lines = append(lines, previewLabelStyle.Render(" Summary"))
		lines = append(lines, previewValueStyle.Render(" "+s.Summary))
	}

	// Conversation preview
	if len(m.previewMsgs) > 0 && m.previewSessID == s.SessionID {
		lines = append(lines, "")
		lines = append(lines, previewDividerStyle.Render(strings.Repeat("─", innerW)))
		lines = append(lines, previewLabelStyle.Render(" Conversation"))

		maxMsgs := (h - len(lines) - 4) // fill remaining space
		if maxMsgs > 15 {
			maxMsgs = 15
		}
		if maxMsgs < 3 {
			maxMsgs = 3
		}

		shown := 0
		for _, msg := range m.previewMsgs {
			if shown >= maxMsgs {
				break
			}
			if msg.Message == nil {
				continue
			}
			text := session.ExtractText(msg.Message.Content)
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			text = util.CleanPrompt(text)
			text = util.Truncate(text, innerW-14)
			ts := msg.Timestamp.Format("15:04")

			var roleStyle lipgloss.Style
			if msg.Message.Role == "user" {
				roleStyle = previewRoleUser
			} else {
				roleStyle = previewRoleAssistant
			}

			tag := "[usr]"
			if msg.Message.Role == "assistant" {
				tag = "[ast]"
			}

			prefix := roleStyle.Render(tag+" "+ts+" ")
			lines = append(lines, prefix+previewMsgText.Render(text))
			shown++
		}
	} else if m.previewSessID != s.SessionID {
		lines = append(lines, "")
		lines = append(lines, statusDescStyle.Render(" Loading..."))
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Width(w).Height(h - 2).Render(content)
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
	parts := []string{
		statusKeyStyle.Render("↑↓") + statusDescStyle.Render(" nav"),
		statusKeyStyle.Render("/") + statusDescStyle.Render(" search"),
		statusKeyStyle.Render("tab") + statusDescStyle.Render(" deep"),
		statusKeyStyle.Render("s") + statusDescStyle.Render(" sort"),
		statusKeyStyle.Render("p") + statusDescStyle.Render(" project"),
		statusKeyStyle.Render("d") + statusDescStyle.Render(" date"),
		statusKeyStyle.Render("⏎") + statusDescStyle.Render(" resume"),
		statusKeyStyle.Render("y") + statusDescStyle.Render(" copy"),
		statusKeyStyle.Render("q") + statusDescStyle.Render(" quit"),
	}
	return statusBarStyle.Width(m.width - 2).Render(strings.Join(parts, "  "))
}
