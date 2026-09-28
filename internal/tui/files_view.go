package tui

import (
	"charm.land/bubbles/v2/viewport"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/charmbracelet/x/ansi"
)

type filesLoaded struct {
	request uint64
	files   []session.FileChange
	err     error
}
type fileLoaded struct {
	request, loadID uint64
	path, text      string
	err             error
}
type fileBrowser struct {
	request, loadID uint64
	entry           session.SessionEntry
	files           []session.FileChange
	cursor          int
	viewport        viewport.Model
	contentFocused  bool
	text            string
	loading         bool
	err             error
}

func (m *Model) openFiles() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	m.stopDeepSearch()
	entry := m.filtered[m.cursor]
	m.requestID++
	request := m.requestID
	m.files = &fileBrowser{request: request, entry: entry, loading: true, viewport: viewport.New()}
	m.sizeFileViewport()
	return func() tea.Msg {
		files, err := session.FileChanges(entry)
		return filesLoaded{request: request, files: files, err: err}
	}
}

func (f *fileBrowser) load() tea.Cmd {
	f.text, f.err = "", nil
	f.viewport.GotoTop()
	f.viewport.SetContent("")
	if len(f.files) == 0 {
		f.loading = false
		return nil
	}
	f.loading = true
	f.loadID++
	request, loadID := f.request, f.loadID
	entry, file := f.entry, f.files[f.cursor]
	return func() tea.Msg {
		var text string
		var err error
		if file.Recoverable {
			var b []byte
			b, err = session.RecoverContent(entry, file.Path)
			text = string(b)
		} else if entry.Provider == provider.Codex {
			var diffs []string
			diffs, err = session.CodexDiff(entry, file.Path)
			text = strings.Join(diffs, "\n\n")
		} else {
			text = "No readable checkpoint or confirmed Write content is available."
		}
		if len(text) > 65536 {
			text = text[:65536] + "\n[Preview limited to 64 KiB]"
		}
		if text == "" && err == nil {
			if file.Recoverable {
				text = "[Empty file]"
			} else {
				text = "[No recorded diff]"
			}
		}
		return fileLoaded{request: request, loadID: loadID, path: file.Path, text: terminalText(text), err: err}
	}
}

func (m Model) handleFileKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.files
	m.sizeFileViewport()
	if msg.String() == "tab" || msg.String() == "shift+tab" {
		f.contentFocused = !f.contentFocused
		return m, nil
	}
	if f.contentFocused && msg.String() != "esc" && msg.String() != "o" && msg.String() != "q" {
		switch msg.String() {
		case "g", "home":
			f.viewport.GotoTop()
			return m, nil
		case "G", "end":
			f.viewport.GotoBottom()
			return m, nil
		}
		var cmd tea.Cmd
		f.viewport, cmd = f.viewport.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "esc", "o":
		m.files = nil
	case "q":
		m.stopDeepSearch()
		return m, tea.Quit
	case "up", "k":
		if f.cursor > 0 {
			f.cursor--
			return m, f.load()
		}
	case "down", "j":
		if f.cursor+1 < len(f.files) {
			f.cursor++
			return m, f.load()
		}
	case "pgdown", "]":
		f.viewport.ScrollDown(max(1, f.viewport.Height()/2))
	case "pgup", "[":
		f.viewport.ScrollUp(max(1, f.viewport.Height()/2))
	}
	return m, nil
}

func (m Model) fileDimensions() (int, int, int, int) {
	listW := m.width * 35 / 100
	previewW := m.width - listW
	listH, previewH := m.height-3, m.height-3
	if m.width < 90 {
		listW = m.width
		previewW = m.width
		listH = 6
		previewH = m.height - 3 - listH
	}
	return listW, previewW, listH, previewH
}

func (m Model) fileHeader(w int) string {
	f := m.files
	if len(f.files) == 0 {
		return ""
	}
	file := f.files[f.cursor]
	label := "Recorded diff · not full file content"
	if f.entry.Provider != provider.Codex {
		label = "Content unavailable"
	}
	if file.Recoverable {
		label = "Historical content · not final filesystem state"
	}
	source := file.RecoverySource
	if source == "" {
		source = "no saved content"
		if f.entry.Provider == provider.Codex {
			source = "codex patch"
		}
	}
	lines := []string{m.theme.previewHeaderStyle.Render(clipLine(file.Path, w)), m.theme.filterActiveStyle.Render(label), fmt.Sprintf("%s · %d revisions · %s", file.Kind, file.Revisions, source)}
	if file.RecoveryEarlierVersion {
		lines = append(lines, "Earlier version: a newer checkpoint is unavailable")
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}

func (m *Model) sizeFileViewport() {
	if m.files == nil {
		return
	}
	_, w, _, h := m.fileDimensions()
	m.files.viewport.SetWidth(max(1, w-4))
	m.files.viewport.SetHeight(max(1, h-3-lipgloss.Height(m.fileHeader(max(1, w-4)))))
}

func (m *Model) setFileContent() {
	f := m.files
	f.viewport.SoftWrap = true
	f.viewport.FillHeight = true
	f.viewport.SetContent(f.text)
	lines := strings.Split(f.text, "\n")
	diff := len(f.files) > 0 && !f.files[f.cursor].Recoverable
	normal, added, deleted, hunk := m.theme.previewMsgText, m.fileKindStyle(session.ChangeAdd), m.fileKindStyle(session.ChangeDelete), m.theme.searchLabelStyle
	f.viewport.StyleLineFunc = func(i int) lipgloss.Style {
		if diff && i < len(lines) {
			switch {
			case strings.HasPrefix(lines[i], "+"):
				return added
			case strings.HasPrefix(lines[i], "-"):
				return deleted
			case strings.HasPrefix(lines[i], "@@"):
				return hunk
			}
		}
		return normal
	}
	muted := m.theme.statusDescStyle
	f.viewport.LeftGutterFunc = func(g viewport.GutterContext) string {
		if g.Soft || g.Index >= g.TotalLines {
			return "     │ "
		}
		return muted.Render(fmt.Sprintf("%4d │ ", g.Index+1))
	}
	m.sizeFileViewport()
}

func (m Model) renderFiles() string {
	f := m.files
	listW, previewW, listH, h := m.fileDimensions()
	header := m.providerBadge(f.entry.Provider) + "  " + m.theme.itemTitleStyle.Render(clipLine("Files / "+f.entry.DisplayTitle(), m.width-12))
	visible := max(1, (listH-2)/2)
	start := max(0, f.cursor-visible+1)
	var rows []string
	for i := start; i < len(f.files) && i < start+visible; i++ {
		file := f.files[i]
		mark := "  "
		style := m.theme.previewValueStyle
		if i == f.cursor {
			mark = "▸ "
			style = style.Background(m.theme.colorBgPanel).Bold(true)
		}
		rows = append(rows, style.Width(listW-4).Render(clipLine(mark+shortPath(file.Path, listW-6), listW-4)), m.fileKindStyle(file.Kind).Render("  "+file.Kind))
	}
	if len(rows) == 0 {
		rows = []string{"No recorded files"}
		if f.loading {
			rows = []string{"Loading recorded files…"}
		}
	}
	leftStyle, rightStyle := m.theme.activePanelStyle, m.theme.panelStyle
	if f.contentFocused {
		leftStyle, rightStyle = rightStyle, leftStyle
	}
	left := leftStyle.Width(listW).Height(listH).MaxHeight(listH).Render(strings.Join(rows, "\n"))
	content := m.fileHeader(max(1, previewW-4))
	if f.err != nil {
		content += "\nUnavailable: " + terminalText(f.err.Error())
	} else if f.loading {
		content += "\nLoading…"
	} else {
		content += fmt.Sprintf("\nRecorded lines · %.0f%%\n", f.viewport.ScrollPercent()*100) + f.viewport.View()
	}
	right := rightStyle.Width(previewW).Height(h).MaxHeight(h).Render(content)
	panels := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	if m.width < 90 {
		panels = lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	footer := m.theme.statusDescStyle.Render(clipLine("Ctrl+k Actions  ↑↓ navigate  Tab focus  Esc back", m.width))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", panels, footer)
}

// Stored text must never become terminal control sequences.
func terminalText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}
func clipLine(s string, w int) string {
	s = strings.NewReplacer("\n", " ", "\t", "    ").Replace(terminalText(s))
	return ansi.Truncate(s, max(1, w), "…")
}

func (m Model) fileKindStyle(kind string) lipgloss.Style {
	switch kind {
	case session.ChangeAdd:
		return lipgloss.NewStyle().Foreground(m.theme.colorSuccess)
	case session.ChangeDelete:
		return lipgloss.NewStyle().Foreground(lipgloss.LightDark(m.dark)(lipgloss.Color("#A4252C"), lipgloss.Color("#FF9A9F")))
	default:
		return lipgloss.NewStyle().Foreground(m.theme.colorWarning)
	}
}
