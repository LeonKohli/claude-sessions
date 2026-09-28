package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/charmbracelet/x/ansi"
)

type readerLoaded struct {
	request  uint64
	messages []session.PreviewMessage
	notice   string
	err      error
}
type conversationReader struct {
	request  uint64
	messages []session.PreviewMessage
	notice   string
	renderID uint64
	raw      bool
	viewport viewport.Model
	find     textinput.Model
	finding  bool
	matches  int
	loading  bool
	markdown *readerRendered
}

func (m *Model) openReader() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	m.stopDeepSearch()
	entry := m.filtered[m.cursor]
	vp := viewport.New(viewport.WithWidth(max(1, m.width-4)), viewport.WithHeight(max(1, m.height-6)))
	vp.FillHeight = true
	vp.HighlightStyle = lipgloss.NewStyle().Background(m.theme.colorWarning).Foreground(lipgloss.Color("#172B3A"))
	vp.SelectedHighlightStyle = vp.HighlightStyle.Bold(true)
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "Find in this conversation"
	input.SetWidth(max(1, m.width-24))
	input.SetVirtualCursor(true)
	m.requestID++
	request := m.requestID
	m.reader = &conversationReader{request: request, viewport: vp, find: input, loading: true}
	if m.searchMode == searchDeep {
		m.reader.find.SetValue(m.searchInput.Value())
	}
	return func() tea.Msg {
		messages := make([]session.PreviewMessage, 0)
		var size int
		var notice string
		err := session.WalkSearchable(context.Background(), entry.Provider, entry.FullPath, func(line session.SearchableLine) bool {
			messages = append(messages, session.PreviewMessage{Role: line.Role, Text: line.Text, Timestamp: line.Timestamp})
			size += len(line.Text)
			if size > 8<<20 {
				notice = "Reader limited to 8 MiB of conversation text; use show for the remaining records"
				return false
			}
			return true
		})
		return readerLoaded{request: request, messages: messages, notice: notice, err: err}
	}
}

func (r *conversationReader) search() {
	r.viewport.ClearHighlights()
	r.matches = 0
	words := strings.Fields(r.find.Value())
	if len(words) == 0 {
		return
	}
	for i := range words {
		words[i] = regexp.QuoteMeta(words[i])
	}
	matches := regexp.MustCompile("(?i)"+strings.Join(words, `\s+`)).FindAllStringIndex(r.viewport.GetContent(), -1)
	r.matches = len(matches)
	r.viewport.SetHighlights(matches)
}

func (m Model) handleReaderKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.reader
	if r.finding {
		switch msg.String() {
		case "esc", "enter":
			r.finding = false
			r.find.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		r.find, cmd = r.find.Update(msg)
		r.search()
		return m, cmd
	}
	switch msg.String() {
	case "esc":
		m.reader = nil
		return m, nil
	case "/":
		r.finding = true
		return m, r.find.Focus()
	case "n":
		r.viewport.HighlightNext()
		return m, nil
	case "N":
		r.viewport.HighlightPrevious()
		return m, nil
	case "o":
		return m, m.openFiles()
	case "m":
		cmd := m.toggleReaderSource()
		return m, cmd
	case "r", "q", "?", "y":
		return m.handleNormalKey(msg)
	case "g", "home":
		r.viewport.GotoTop()
		return m, nil
	case "G", "end":
		r.viewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	r.viewport, cmd = r.viewport.Update(msg)
	return m, cmd
}

func (m *Model) toggleReaderSource() tea.Cmd {
	m.reader.raw = !m.reader.raw
	return m.styleReader()
}

func (m Model) renderReader() string {
	r := m.reader
	s := m.filtered[m.cursor]
	header := m.theme.itemTitleStyle.Render(clipLine(s.DisplayTitle(), m.width-4))
	meta := m.providerBadge(s.Provider) + "  " + m.theme.statusDescStyle.Render(clipLine(s.ProjectPath, m.width-15))
	search := r.find.View() + m.theme.statusDescStyle.Render(fmt.Sprintf("  %d matches", r.matches))
	content := r.viewport.View()
	if r.loading {
		content = "Loading conversation…"
	}
	mode := "Markdown"
	if r.raw {
		mode = "Source"
	}
	footer := fmt.Sprintf("Ctrl+k Actions  / find  n/N matches  m %s  Esc back    %.0f%%", mode, r.viewport.ScrollPercent()*100)
	if r.finding {
		footer = "Find in conversation · Enter finish · Esc back · Ctrl+k Actions"
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, meta, search, m.theme.activePanelStyle.Width(m.width).Height(m.height-4).Render(content), m.theme.statusDescStyle.Render(clipLine(footer, m.width)))
}

type readerRendered struct {
	request, renderID uint64
	viewport          viewport.Model
	raw, dark         bool
}

func (m *Model) styleReader() tea.Cmd {
	r := m.reader
	if r.messages == nil {
		return nil
	}
	r.renderID++
	request, renderID := r.request, r.renderID
	if cached := r.markdown; !r.raw && cached != nil && cached.dark == m.dark && cached.viewport.Width() == r.viewport.Width() {
		vp := cached.viewport
		vp.SetHeight(r.viewport.Height())
		m.setReaderViewport(vp)
		return nil
	}
	r.loading = true
	if m.readerRendering {
		return nil
	}
	m.readerRendering = true
	render := m.renderConversation(r.messages, r.viewport.Width(), r.raw)
	notice, width, height := r.notice, r.viewport.Width(), r.viewport.Height()
	raw, dark := r.raw, m.dark
	highlight := lipgloss.NewStyle().Background(m.theme.colorWarning).Foreground(lipgloss.Color("#172B3A"))
	return func() tea.Msg {
		text := render()
		if notice != "" {
			text += "\n" + ansi.Hardwrap(terminalText(notice), width, true)
		}
		vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
		vp.FillHeight = true
		lines := strings.Split(text, "\n")
		vp.SetContent(ansi.Strip(text))
		vp.StyleLineFunc = func(i int) lipgloss.Style {
			if i >= len(lines) {
				return lipgloss.NewStyle()
			}
			return lipgloss.NewStyle().Transform(func(string) string { return lines[i] })
		}
		vp.HighlightStyle = highlight
		vp.SelectedHighlightStyle = highlight.Bold(true)
		return readerRendered{request: request, renderID: renderID, viewport: vp, raw: raw, dark: dark}
	}
}

func (m *Model) setReaderViewport(vp viewport.Model) {
	r := m.reader
	offset := r.viewport.YOffset()
	r.viewport = vp
	r.viewport.SetYOffset(offset)
	r.find.SetStyles(textinput.DefaultStyles(m.dark))
	r.loading = false
	r.search()
}
