package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/charmbracelet/x/ansi"
)

type conversationStyle struct {
	theme    theme
	dark     bool
	provider provider.Kind
}

func (m conversationStyle) text(messages []session.PreviewMessage, width int, raw bool) (string, error) {
	style := styles.LightStyleConfig
	if m.dark {
		style = styles.DarkStyleConfig
	}
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	accent := "#005EA8"
	answerBackground := "#EDF1F5"
	promptBackground := "#D7EBFC"
	if m.dark {
		accent = "#79C0FF"
		answerBackground, promptBackground = "#1C252E", "#203D56"
	}
	// The enclosing card supplies base colors; Markdown only styles its accents.
	style.Document.Color = nil
	style.Document.BackgroundColor = nil
	style.CodeBlock.Chroma = nil
	style.CodeBlock.Theme = "github"
	if m.dark {
		style.CodeBlock.Theme = "github-dark"
	}
	style.Code.Color = &accent
	style.Heading.Color = &accent
	style.H1 = style.Heading
	style.H2.Prefix, style.H3.Prefix = "", ""
	style.H4.Prefix, style.H5.Prefix, style.H6.Prefix = "", "", ""
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(max(1, width-2)), glamour.WithPreservedNewLines())
	if err != nil {
		return "", err
	}
	var blocks []string
	for _, msg := range messages {
		text := terminalText(msg.Text)
		if text == "" {
			continue
		}
		label := "● Claude · Assistant"
		header := m.theme.badgeClaudeStyle
		if m.provider == provider.Codex {
			label, header = "● Codex · Assistant", m.theme.badgeCodexStyle
		}
		if msg.Role == "user" {
			label, header = "› You · Prompt", m.theme.previewRoleUser
		}
		if !msg.Timestamp.IsZero() {
			label += " · " + msg.Timestamp.Format("15:04")
		}
		var body string
		background := lipgloss.Color(answerBackground)
		if msg.Role == "user" {
			background = lipgloss.Color(promptBackground)
			body = ansi.Wordwrap(text, max(1, width-2), "")
		} else if raw {
			body = ansi.Wordwrap(text, max(1, width-2), "")
		} else {
			body, err = renderer.Render(text)
			if err != nil {
				return "", err
			}
			body = strings.Trim(body, "\n")
		}
		card := lipgloss.NewStyle().Foreground(m.theme.colorText).Background(background).Width(width).Padding(1)
		base := ansi.Style{}.ForegroundColor(m.theme.colorText).BackgroundColor(background).String()
		// Markdown resets must return to the card's colors, not the terminal's.
		content := header.Background(background).Render(label) + "\n\n" + body
		content = strings.NewReplacer(ansi.ResetStyle, ansi.ResetStyle+base, "\x1b[0m", "\x1b[0m"+base).Replace(content)
		blocks = append(blocks, card.Render(content))
	}
	return ansi.Hardwrap(strings.Join(blocks, "\n\n"), max(1, width), true), nil
}

// Render commands own their message list and display settings.
func (m Model) renderConversation(messages []session.PreviewMessage, width int, raw bool) func() string {
	snapshot := conversationStyle{dark: m.dark, theme: m.theme}
	if len(m.filtered) > 0 {
		snapshot.provider = m.filtered[m.cursor].Provider
	}
	messages = append([]session.PreviewMessage(nil), messages...)
	return func() string {
		text, err := snapshot.text(messages, width, raw)
		if err != nil {
			return "Conversation rendering failed: " + terminalText(err.Error())
		}
		return text
	}
}

type previewRendered struct {
	request, renderID uint64
	text              string
}

func (m *Model) stylePreview() tea.Cmd {
	if len(m.previewMsgs) == 0 {
		return nil
	}
	m.previewRenderID++
	if m.previewRendering {
		return nil
	}
	m.previewRendering = true
	request, renderID := m.previewRequest, m.previewRenderID
	render := m.renderConversation(m.previewMsgs, m.preview.Width(), false)
	return func() tea.Msg { return previewRendered{request, renderID, render()} }
}
