package tui

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/LeonKohli/claude-sessions/internal/util"
)

// providerFilter cycles all → claude → codex.
type providerFilter int

const (
	providerAll providerFilter = iota
	providerClaude
	providerCodex
	providerFilterCount
)

func (p providerFilter) String() string {
	switch p {
	case providerClaude:
		return "claude"
	case providerCodex:
		return "codex"
	default:
		return "all"
	}
}

// matches reports whether an entry passes this filter.
func (p providerFilter) matches(k provider.Kind) bool {
	switch p {
	case providerClaude:
		return k == provider.Claude
	case providerCodex:
		return k == provider.Codex
	default:
		return true
	}
}

type searchMode int

const (
	searchFuzzy searchMode = iota
	searchDeep
)

type dateFilter int

const (
	dateAll dateFilter = iota
	dateToday
	dateWeek
	dateMonth
)

func (d dateFilter) String() string {
	switch d {
	case dateToday:
		return "today"
	case dateWeek:
		return "week"
	case dateMonth:
		return "month"
	default:
		return "all"
	}
}

type sortMode int

const (
	sortModified sortMode = iota
	sortCreated
	sortTokens
	sortMessages
	sortDuration
	sortSize
	sortModeCount // sentinel for cycling
)

func (s sortMode) String() string {
	switch s {
	case sortCreated:
		return "created"
	case sortTokens:
		return "tokens"
	case sortMessages:
		return "msgs"
	case sortDuration:
		return "duration"
	case sortSize:
		return "size"
	default:
		return "modified"
	}
}

type Model struct {
	// Data
	allSessions  []session.SessionEntry
	filtered     []session.SessionEntry
	sessionMap   map[string]*session.SessionEntry // for enrichment updates
	cursor       int
	scrollOffset int

	// Search
	searchInput   textinput.Model
	searchActive  bool
	searchMode    searchMode
	debounceID    int
	deepResults   map[string][]string // sessionID -> snippets
	deepSearching bool
	deepCancel    context.CancelFunc

	// Filters
	projectFilter  string // empty = all
	projects       []string
	projectIdx     int
	dateFilter     dateFilter
	providerFilter providerFilter
	showSubagents  bool
	sortBy         sortMode

	// Preview
	previewMsgs       []session.PreviewMessage
	previewSessID     string
	previewEnrichment *session.EnrichmentData
	previewScroll     int

	// UI state
	width  int
	height int
	toast  string
	ready  bool

	// Resume action
	resumeSessionID string
	resumeProject   string
	resumeProvider  provider.Kind
}

func NewModel(sessions []session.SessionEntry) Model {
	ti := textinput.New()
	ti.Placeholder = "Search sessions..."
	ti.CharLimit = 200

	// Extract unique projects
	projSet := make(map[string]bool)
	for _, s := range sessions {
		if s.ProjectPath != "" {
			projSet[s.ProjectPath] = true
		}
	}
	var projects []string
	for p := range projSet {
		projects = append(projects, p)
	}
	sort.Strings(projects)

	// Build lookup map
	smap := make(map[string]*session.SessionEntry, len(sessions))
	for i := range sessions {
		smap[sessions[i].SessionID] = &sessions[i]
	}

	m := Model{
		allSessions: sessions,
		sessionMap:  smap,
		searchInput: ti,
		projects:    projects,
	}
	m.applyFilters()
	return m
}

func (m Model) Init() tea.Cmd {
	// Load preview for first item on startup
	return m.triggerPreview()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.ready = true
			// Trigger initial preview now that we have dimensions
			return m, m.triggerPreview()
		}
		return m, nil

	case tea.KeyMsg:
		if m.searchActive {
			return m.handleSearchKey(msg)
		}
		return m.handleNormalKey(msg)

	case PreviewLoadedMsg:
		if msg.SessionID == m.previewSessID {
			m.previewMsgs = msg.Messages
			m.previewEnrichment = msg.Enrichment
			// Also update the session entry with enrichment data
			if msg.Enrichment != nil {
				if entry, ok := m.sessionMap[msg.SessionID]; ok {
					entry.TotalInputTokens = msg.Enrichment.TotalInputTokens
					entry.TotalOutputTokens = msg.Enrichment.TotalOutputTokens
					entry.CacheReadTokens = msg.Enrichment.CacheReadTokens
					entry.CacheWriteTokens = msg.Enrichment.CacheWriteTokens
					entry.Model = msg.Enrichment.Model
					entry.ToolsUsed = msg.Enrichment.ToolsUsed
					entry.FilesModified = msg.Enrichment.FilesModified
					entry.Enriched = true
					if msg.Enrichment.MessageCount > 0 {
						entry.MessageCount = msg.Enrichment.MessageCount
					}
				}
			}
		}
		return m, nil

	case DeepSearchResultMsg:
		m.deepSearching = false
		m.deepResults = make(map[string][]string)
		for _, r := range msg.Results {
			m.deepResults[r.Session.SessionID] = r.Snippets
		}
		m.applyFilters()
		return m, nil

	case ToastMsg:
		m.toast = msg.Text
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearToastMsg{}
		})

	case ClearToastMsg:
		m.toast = ""
		return m, nil

	case SearchDebounceMsg:
		if msg.ID == m.debounceID && m.searchMode == searchDeep {
			m.deepSearching = true
			return m, m.runDeepSearch(msg.Query)
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.ensureCursorVisible()
			m.previewSessID = ""
			m.previewMsgs = nil
			return m, m.triggerPreview()
		}

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.ensureCursorVisible()
			m.previewSessID = ""
			m.previewMsgs = nil
			return m, m.triggerPreview()
		}

	case key.Matches(msg, keys.HalfUp):
		listH := m.listHeight()
		m.cursor -= listH / 2
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.HalfDown):
		listH := m.listHeight()
		m.cursor += listH / 2
		if m.cursor >= len(m.filtered) {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.GotoTop):
		m.cursor = 0
		m.scrollOffset = 0
		return m, m.triggerPreview()

	case key.Matches(msg, keys.GotoBottom):
		m.cursor = len(m.filtered) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.PageUp):
		listH := m.listHeight()
		m.cursor -= listH
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.PageDown):
		listH := m.listHeight()
		m.cursor += listH
		if m.cursor >= len(m.filtered) {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.Enter):
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			s := m.filtered[m.cursor]
			// Claude writes subagent transcripts under an agent-<uuid> name
			// that its own --resume cannot resolve. Codex spawned threads are
			// real threads and resume fine.
			if s.Provider == provider.Claude && s.IsSubagent {
				return m, func() tea.Msg {
					return ToastMsg{Text: "subagent transcripts aren't resumable"}
				}
			}
			m.resumeSessionID = s.SessionID
			m.resumeProject = s.ProjectPath
			m.resumeProvider = s.Provider
			return m, tea.Quit
		}

	case key.Matches(msg, keys.Search):
		m.searchActive = true
		m.searchInput.Focus()
		return m, textinput.Blink

	case key.Matches(msg, keys.Tab):
		if m.searchMode == searchFuzzy {
			m.searchMode = searchDeep
		} else {
			m.searchMode = searchFuzzy
		}
		if m.searchInput.Value() != "" {
			m.applyFilters()
		}
		return m, nil

	case key.Matches(msg, keys.CopyUUID):
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			s := m.filtered[m.cursor]
			if err := util.CopyToClipboard(s.SessionID); err == nil {
				return m, func() tea.Msg {
					return ToastMsg{Text: "UUID copied!"}
				}
			} else {
				return m, func() tea.Msg {
					return ToastMsg{Text: err.Error()}
				}
			}
		}

	case key.Matches(msg, keys.Project):
		m.projectIdx++
		if m.projectIdx > len(m.projects) {
			m.projectIdx = 0
		}
		if m.projectIdx == 0 {
			m.projectFilter = ""
		} else {
			m.projectFilter = m.projects[m.projectIdx-1]
		}
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.Sort):
		m.sortBy = (m.sortBy + 1) % sortModeCount
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.DateFilter):
		m.dateFilter = (m.dateFilter + 1) % 4
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.Provider):
		m.providerFilter = (m.providerFilter + 1) % providerFilterCount
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.Subagents):
		m.showSubagents = !m.showSubagents
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.Escape):
		if m.searchInput.Value() != "" {
			m.searchInput.SetValue("")
			m.deepResults = nil
			m.cursor = 0
			m.scrollOffset = 0
			m.applyFilters()
			m.previewSessID = ""
			m.previewMsgs = nil
			return m, m.triggerPreview()
		}
	}

	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searchActive = false
		m.searchInput.Blur()
		return m, nil

	case tea.KeyEnter:
		m.searchActive = false
		m.searchInput.Blur()
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		if m.searchMode == searchDeep && m.searchInput.Value() != "" {
			m.deepSearching = true
			return m, m.runDeepSearch(m.searchInput.Value())
		}
		return m, m.triggerPreview()

	case tea.KeyTab:
		if m.searchMode == searchFuzzy {
			m.searchMode = searchDeep
		} else {
			m.searchMode = searchFuzzy
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)

		if m.searchMode == searchFuzzy {
			m.cursor = 0
			m.scrollOffset = 0
			m.applyFilters()
			previewCmd := m.triggerPreview()
			return m, tea.Batch(cmd, previewCmd)
		}

		if m.searchMode == searchDeep && m.searchInput.Value() != "" {
			m.debounceID++
			id := m.debounceID
			query := m.searchInput.Value()
			debouncCmd := tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
				return SearchDebounceMsg{Query: query, ID: id}
			})
			return m, tea.Batch(cmd, debouncCmd)
		}

		return m, cmd
	}
}

func (m *Model) applyFilters() {
	query := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	m.filtered = nil

	for _, s := range m.allSessions {
		if s.IsSubagent && !m.showSubagents {
			continue
		}

		if !m.providerFilter.matches(s.Provider) {
			continue
		}

		if m.projectFilter != "" && s.ProjectPath != m.projectFilter {
			continue
		}

		if !m.matchesDateFilter(s) {
			continue
		}

		if m.searchMode == searchDeep && m.deepResults != nil {
			if _, ok := m.deepResults[s.SessionID]; !ok {
				continue
			}
		}

		if query != "" && m.searchMode == searchFuzzy {
			if !smartMatch(query, s) {
				continue
			}
		}

		m.filtered = append(m.filtered, s)
	}

	// Apply sort
	switch m.sortBy {
	case sortModified:
		sort.Slice(m.filtered, func(i, j int) bool {
			return m.filtered[i].Modified.After(m.filtered[j].Modified)
		})
	case sortCreated:
		sort.Slice(m.filtered, func(i, j int) bool {
			return m.filtered[i].Created.After(m.filtered[j].Created)
		})
	case sortTokens:
		sort.Slice(m.filtered, func(i, j int) bool {
			return m.filtered[i].TotalTokens() > m.filtered[j].TotalTokens()
		})
	case sortMessages:
		sort.Slice(m.filtered, func(i, j int) bool {
			return m.filtered[i].EstimatedMessages() > m.filtered[j].EstimatedMessages()
		})
	case sortDuration:
		sort.Slice(m.filtered, func(i, j int) bool {
			return m.filtered[i].Duration() > m.filtered[j].Duration()
		})
	case sortSize:
		sort.Slice(m.filtered, func(i, j int) bool {
			return m.filtered[i].FileSize > m.filtered[j].FileSize
		})
	}

	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// smartMatch does word-split matching: all query words must appear
// as substrings in the combined searchable text.
func smartMatch(query string, s session.SessionEntry) bool {
	searchable := strings.ToLower(
		s.Summary + " " + s.FirstPrompt + " " +
			s.ProjectPath + " " + s.GitBranch + " " + s.SessionID + " " +
			s.Provider.String() + " " + s.Model + " " + s.AgentLabel)

	words := strings.Fields(query)
	for _, w := range words {
		if !strings.Contains(searchable, w) {
			return false
		}
	}
	return true
}

func (m *Model) matchesDateFilter(s session.SessionEntry) bool {
	now := time.Now()
	switch m.dateFilter {
	case dateToday:
		y, mo, d := now.Date()
		start := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
		return s.Modified.After(start)
	case dateWeek:
		start := now.AddDate(0, 0, -7)
		return s.Modified.After(start)
	case dateMonth:
		start := now.AddDate(0, -1, 0)
		return s.Modified.After(start)
	}
	return true
}

func (m *Model) ensureCursorVisible() {
	itemH := 3
	visibleItems := m.listHeight() / itemH
	if visibleItems < 1 {
		visibleItems = 1
	}

	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleItems {
		m.scrollOffset = m.cursor - visibleItems + 1
	}
}

func (m Model) listHeight() int {
	return m.height - 6
}

// triggerPreview sets previewSessID on the model and returns a cmd
// to load the preview messages + enrichment asynchronously.
func (m *Model) triggerPreview() tea.Cmd {
	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		m.previewSessID = ""
		m.previewMsgs = nil
		m.previewEnrichment = nil
		return nil
	}
	s := m.filtered[m.cursor]
	if s.SessionID == m.previewSessID && m.previewMsgs != nil {
		return nil // already loaded
	}
	m.previewSessID = s.SessionID
	m.previewMsgs = nil
	m.previewEnrichment = nil
	path := s.FullPath
	sid := s.SessionID
	kind := s.Provider
	enriched := s.Enriched
	return func() tea.Msg {
		msgs, _ := session.ReadPreview(kind, path, 30)
		var enrichment *session.EnrichmentData
		if !enriched {
			enrichment, _ = session.Enrich(kind, path)
		}
		return PreviewLoadedMsg{SessionID: sid, Messages: msgs, Enrichment: enrichment}
	}
}

func (m Model) runDeepSearch(query string) tea.Cmd {
	if query == "" {
		return nil
	}

	if m.deepCancel != nil {
		m.deepCancel()
	}

	sessions := m.allSessions
	return func() tea.Msg {
		results := deepSearch(sessions, query)
		return DeepSearchResultMsg{Query: query, Results: results}
	}
}

// ExecResume is called after tea.Program exits to hand off to the agent CLI.
//
// Both agents are launched from the session's own directory. Claude needs it to
// locate the transcript at all; Codex would otherwise notice the mismatch and
// interrupt the resume with an interactive working-directory prompt.
func (m Model) ExecResume() {
	if m.resumeSessionID == "" {
		return
	}

	if m.resumeProject != "" {
		os.Chdir(m.resumeProject)
	}

	bin, argv := m.resumeProvider.ResumeArgv(m.resumeSessionID)
	binPath, err := lookPath(bin)
	if err != nil {
		os.Stderr.WriteString("Could not find " + bin + " on PATH\n")
		os.Exit(1)
	}

	syscall.Exec(binPath, argv, os.Environ())
}

func lookPath(bin string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, bin)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}
