package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
	requestID        uint64
	previewRequest   uint64
	previewRenderID  uint64
	previewText      string
	previewRendering bool
	readerRendering  bool

	theme theme
	dark  bool
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
	dateFilter     dateFilter
	providerFilter providerFilter
	showSubagents  bool
	sortBy         sortMode

	// Preview
	previewMsgs     []session.PreviewMessage
	previewSessID   string
	preview         viewport.Model
	previewLoading  bool
	previewErr      error
	previewFiles    []session.FileChange
	previewFilesErr error

	reader         *conversationReader
	previewFocused bool
	files          *fileBrowser
	menu           *actionMenu

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
	ti.Prompt = ""
	ti.SetVirtualCursor(true)

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
		smap[sessions[i].ReferenceID()] = &sessions[i]
	}

	m := Model{
		theme: newTheme(true), dark: true,
		preview:     viewport.New(),
		allSessions: sessions,
		sessionMap:  smap,
		searchInput: ti,
		projects:    projects,
	}
	m.applyFilters()
	m.preview.SoftWrap = true
	m.preview.FillHeight = true
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.theme = newTheme(m.dark)
		m.styleMenu()
		m.refreshPreview()
		previewCmd := m.stylePreview()
		var readerCmd tea.Cmd
		m.searchInput.SetStyles(textinput.DefaultStyles(m.dark))
		if m.files != nil {
			m.setFileContent()
		}
		if m.reader != nil {
			m.reader.find.SetStyles(textinput.DefaultStyles(m.dark))
			readerCmd = m.styleReader()
		}
		return m, tea.Batch(previewCmd, readerCmd)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.sizeMenu()
		m.searchInput.SetWidth(m.searchFieldWidth())
		m.ensureCursorVisible()
		m.sizeFileViewport()
		m.refreshPreview()
		previewCmd := m.stylePreview()
		var readerCmd tea.Cmd
		if m.reader != nil {
			m.reader.viewport.SetWidth(max(1, m.width-4))
			m.reader.viewport.SetHeight(max(1, m.height-6))
			m.reader.find.SetWidth(max(1, m.width-24))
			readerCmd = m.styleReader()
		}
		if !m.ready {
			m.ready = true
			// Trigger initial preview now that we have dimensions
			return m, m.triggerPreview()
		}
		return m, tea.Batch(previewCmd, readerCmd)

	case tea.PasteMsg:
		if m.menu != nil {
			return m.handleMenu(msg)
		}
		if m.files != nil {
			return m, nil
		}
		if m.reader != nil {
			if !m.reader.finding {
				return m, nil
			}
			var cmd tea.Cmd
			m.reader.find, cmd = m.reader.find.Update(msg)
			m.reader.search()
			return m, cmd
		}
		if m.searchActive {
			return m.updateSearchInput(msg)
		}
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.stopDeepSearch()
			return m, tea.Quit
		}
		if m.menu != nil {
			return m.handleMenu(msg)
		}
		if msg.String() == "ctrl+k" || msg.String() == "f1" || (msg.String() == "?" && !m.editingText()) {
			return m, m.openActions()
		}
		if m.files != nil {
			return m.handleFileKey(msg)
		}
		if m.reader != nil {
			return m.handleReaderKey(msg)
		}
		if m.searchActive {
			return m.handleSearchKey(msg)
		}
		return m.handleNormalKey(msg)

	case readerLoaded:
		if m.reader != nil && m.reader.request == msg.request {
			m.reader.loading = false
			m.reader.messages = msg.messages
			m.reader.notice = msg.notice
			if msg.err != nil {
				m.reader.notice += "\nConversation unavailable: " + terminalText(msg.err.Error())
			}
			cmd := m.styleReader()
			return m, cmd
		}
		return m, nil
	case readerRendered:
		m.readerRendering = false
		if m.reader != nil && m.reader.request == msg.request && m.reader.renderID == msg.renderID {
			if !msg.raw {
				m.reader.markdown = &msg
			}
			m.setReaderViewport(msg.viewport)
		} else if m.reader != nil && m.reader.loading {
			cmd := m.styleReader()
			return m, cmd
		}
		return m, nil
	case previewRendered:
		m.previewRendering = false
		if m.previewRequest == msg.request && m.previewRenderID == msg.renderID {
			m.previewText = msg.text
			m.refreshPreview()
		} else {
			cmd := m.stylePreview()
			return m, cmd
		}
		return m, nil
	case filesLoaded:
		if m.files != nil && msg.request == m.files.request {
			m.files.files = msg.files
			m.files.err = msg.err
			m.files.loading = false
			if msg.err == nil {
				return m, m.files.load()
			}
		}
		return m, nil
	case fileLoaded:
		if m.files != nil && msg.request == m.files.request && msg.loadID == m.files.loadID && len(m.files.files) > 0 && msg.path == m.files.files[m.files.cursor].Path {
			m.files.text = msg.text
			m.setFileContent()
			m.files.err = msg.err
			m.files.loading = false
		}
		return m, nil
	case PreviewLoadedMsg:
		if msg.Request != m.previewRequest {
			return m, nil
		}
		if msg.ReferenceID == m.previewSessID {
			m.previewLoading = false
			m.previewErr = msg.Err
			m.previewMsgs = msg.Messages
			m.previewFiles = msg.Files
			m.previewFilesErr = msg.FilesErr
			// Also update the session entry with enrichment data
			if msg.Enrichment != nil {
				if entry, ok := m.sessionMap[msg.ReferenceID]; ok {
					entry.TotalInputTokens = msg.Enrichment.TotalInputTokens
					entry.TotalOutputTokens = msg.Enrichment.TotalOutputTokens
					entry.CacheReadTokens = msg.Enrichment.CacheReadTokens
					entry.CacheWriteTokens = msg.Enrichment.CacheWriteTokens
					entry.Model = msg.Enrichment.Model
					entry.ToolsUsed = msg.Enrichment.ToolsUsed
					entry.FilesModified = msg.Enrichment.FilesModified
					entry.Enriched = true
					entry.MessageCount = msg.Enrichment.MessageCount
					for i := range m.filtered {
						if m.filtered[i].FullPath == entry.FullPath {
							m.filtered[i] = *entry
						}
					}
				}
			}
		}
		m.refreshPreview()
		cmd := m.stylePreview()
		return m, cmd

	case DeepSearchResultMsg:
		if msg.ID != m.debounceID || msg.Query != m.searchInput.Value() || m.searchMode != searchDeep || msg.Query == "" {
			return m, nil
		}
		m.deepSearching = false
		if m.deepCancel != nil {
			m.deepCancel()
			m.deepCancel = nil
		}
		m.toast = ""
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		}
		m.deepResults = make(map[string][]string)
		for _, r := range msg.Results {
			m.deepResults[r.Session.ReferenceID()] = r.Snippets
		}
		m.applyFilters()
		m.cursor = 0
		m.scrollOffset = 0
		return m, m.triggerPreview()

	case ToastMsg:
		m.toast = msg.Text
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearToastMsg{}
		})

	case ClearToastMsg:
		m.toast = ""
		return m, nil

	case SearchDebounceMsg:
		if msg.ID == m.debounceID && m.searchMode == searchDeep && msg.Query == m.searchInput.Value() {
			m.deepSearching = true
			return m, m.runDeepSearch(msg.Query)
		}
		return m, nil
	}

	if m.menu != nil {
		return m.handleMenu(msg)
	}
	var cmd tea.Cmd
	if m.reader != nil && m.reader.finding {
		m.reader.find, cmd = m.reader.find.Update(msg)
	} else if m.searchActive {
		m.searchInput, cmd = m.searchInput.Update(msg)
	}
	return m, cmd
}

func (m Model) handleNormalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "tab" || msg.String() == "shift+tab" {
		m.searchActive = false
		m.searchInput.Blur()
		m.previewFocused = !m.previewFocused
		return m, nil
	}
	if m.previewFocused {
		switch msg.String() {
		case "g", "home":
			m.preview.GotoTop()
			return m, nil
		case "G", "end":
			m.preview.GotoBottom()
			return m, nil
		case "up", "k":
			m.scrollPreview(-1)
			return m, nil
		case "down", "j":
			m.scrollPreview(1)
			return m, nil
		case "pgup", "ctrl+u":
			m.scrollPreview(-max(1, m.height/2))
			return m, nil
		case "pgdown", "ctrl+d":
			m.scrollPreview(max(1, m.height/2))
			return m, nil
		case "esc":
			m.previewFocused = false
			return m, nil
		}
	}
	switch {
	case key.Matches(msg, keys.Enter):
		return m, m.openReader()
	case msg.String() == "o":
		return m, m.openFiles()
	case msg.String() == "]":
		m.scrollPreview(max(1, m.height/2))
		return m, nil
	case msg.String() == "[":
		m.scrollPreview(-max(1, m.height/2))
		return m, nil
	case key.Matches(msg, keys.Quit):
		m.stopDeepSearch()
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
		listH := max(1, m.listHeight()/m.listItemHeight())
		m.cursor -= listH / 2
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.HalfDown):
		listH := max(1, m.listHeight()/m.listItemHeight())
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
		listH := max(1, m.listHeight()/m.listItemHeight())
		m.cursor -= listH
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.PageDown):
		listH := max(1, m.listHeight()/m.listItemHeight())
		m.cursor += listH
		if m.cursor >= len(m.filtered) {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureCursorVisible()
		return m, m.triggerPreview()

	case msg.String() == "r":
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

	case msg.String() == "ctrl+s":
		cmd := m.toggleSearchMode()
		return m, cmd

	case key.Matches(msg, keys.CopyUUID):
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			id := m.filtered[m.cursor].ReferenceID()
			return m, func() tea.Msg {
				if err := util.CopyToClipboard(id); err != nil {
					return ToastMsg{Text: err.Error()}
				}
				return ToastMsg{Text: "Session ID copied"}
			}
		}

	case key.Matches(msg, keys.Project):
		return m, m.openProjectMenu()
	case key.Matches(msg, keys.Sort):
		return m, m.openSortMenu()
	case key.Matches(msg, keys.DateFilter):
		return m, m.openDateMenu()
	case key.Matches(msg, keys.Provider):
		return m, m.openProviderMenu()

	case key.Matches(msg, keys.Subagents):
		m.showSubagents = !m.showSubagents
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilters()
		return m, m.triggerPreview()

	case key.Matches(msg, keys.Escape):
		if m.searchInput.Value() != "" {
			m.stopDeepSearch()
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

func (m Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "down", "ctrl+n", "ctrl+p":
		if msg.String() == "ctrl+n" {
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		}
		if msg.String() == "ctrl+p" {
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		}
		m.previewFocused = false
		return m.handleNormalKey(msg)
	case "esc":
		m.searchActive = false
		m.searchInput.Blur()
		return m, nil

	case "enter":
		m.searchActive = false
		m.searchInput.Blur()
		return m, m.openReader()
	case "tab":
		m.searchActive = false
		m.searchInput.Blur()
		m.previewFocused = true
		return m, nil
	case "ctrl+s":
		cmd := m.toggleSearchMode()
		return m, cmd

	default:
		return m.updateSearchInput(msg)
	}
}

func (m Model) updateSearchInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	oldQuery := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if oldQuery == m.searchInput.Value() {
		return m, cmd
	}
	m.stopDeepSearch()
	m.deepResults = nil

	if m.searchMode == searchFuzzy || m.searchInput.Value() == "" {
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
			if _, ok := m.deepResults[s.ReferenceID()]; !ok {
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
	itemH := m.listItemHeight()
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
	h := m.height - lipgloss.Height(m.renderSearchBar()) - lipgloss.Height(m.renderStatusBar())
	if m.width < 90 {
		return 4
	}
	return max(0, h-3)
}

// triggerPreview sets previewSessID on the model and returns a cmd
// to load the preview messages + enrichment asynchronously.
func (m *Model) triggerPreview() tea.Cmd {
	if len(m.filtered) > 0 && m.cursor < len(m.filtered) && m.filtered[m.cursor].ReferenceID() == m.previewSessID && m.previewMsgs != nil {
		return nil
	}
	m.requestID++
	m.previewRequest = m.requestID
	request := m.previewRequest
	m.previewRenderID++
	m.previewText = ""
	m.previewErr = nil
	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		m.previewLoading = false
		m.previewSessID = ""
		m.previewMsgs = nil
		m.refreshPreview()
		return nil
	}
	s := m.filtered[m.cursor]
	m.preview.GotoTop()
	m.previewSessID = s.ReferenceID()
	m.previewLoading = true
	m.previewFiles = nil
	m.previewFilesErr = nil
	m.previewMsgs = nil
	m.refreshPreview()
	path := s.FullPath
	sid := s.ReferenceID()
	kind := s.Provider
	enriched := s.Enriched
	return func() tea.Msg {
		msgs, readErr := session.ReadPreview(kind, path, 30)
		var enrichment *session.EnrichmentData
		var enrichErr error
		if !enriched && readErr == nil {
			enrichment, enrichErr = session.Enrich(kind, path)
			if enrichErr != nil {
				enrichment = nil
			}
		}
		files, filesErr := session.FileChanges(s)
		return PreviewLoadedMsg{Request: request, ReferenceID: sid, Messages: msgs, Enrichment: enrichment, Files: files, FilesErr: filesErr, Err: errors.Join(readErr, enrichErr)}
	}
}

func (m *Model) stopDeepSearch() {
	m.debounceID++
	if m.deepCancel != nil {
		m.deepCancel()
		m.deepCancel = nil
	}
	m.deepSearching = false
}

func (m *Model) toggleSearchMode() tea.Cmd {
	m.stopDeepSearch()
	m.searchMode = (m.searchMode + 1) % 2
	m.deepResults = nil
	m.cursor, m.scrollOffset = 0, 0
	m.applyFilters()
	if m.searchMode == searchDeep && m.searchInput.Value() != "" {
		return m.runDeepSearch(m.searchInput.Value())
	}
	return m.triggerPreview()
}

func (m *Model) runDeepSearch(query string) tea.Cmd {
	m.stopDeepSearch()
	if query == "" {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.deepCancel = cancel
	m.deepSearching = true
	id := m.debounceID
	sessions := append([]session.SessionEntry(nil), m.allSessions...)
	return func() tea.Msg {
		results, err := deepSearch(ctx, sessions, query)
		return DeepSearchResultMsg{ID: id, Query: query, Results: results, Err: err}
	}
}

// ExecResume is called after tea.Program exits to hand off to the agent CLI.
//
// Both agents are launched from the session's own directory. Claude needs it to
// locate the transcript at all; Codex would otherwise notice the mismatch and
// interrupt the resume with an interactive working-directory prompt.
func (m Model) ExecResume() error {
	if m.resumeSessionID == "" {
		return nil
	}

	if m.resumeProject != "" {
		if err := os.Chdir(m.resumeProject); err != nil {
			return fmt.Errorf("resume in %q: %w", m.resumeProject, err)
		}
	}

	bin, argv := m.resumeProvider.ResumeArgv(m.resumeSessionID)
	binPath, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("resume %s: %w", bin, err)
	}
	if err := syscall.Exec(binPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("resume %s: %w", bin, err)
	}
	return nil
}

func (m *Model) refreshPreview() {
	_, w, _, h := m.panelDimensions()
	m.preview.SetWidth(max(1, w-4))
	m.preview.SetHeight(max(1, h-3))
	m.preview.SetContent(ansi.Wordwrap(m.previewContent(w, h), max(1, w-4), ""))
}

func (m *Model) scrollPreview(delta int) {
	if delta > 0 {
		m.preview.ScrollDown(delta)
	} else {
		m.preview.ScrollUp(-delta)
	}
}

func (m Model) listItemHeight() int {
	if m.searchMode == searchDeep && m.deepResults != nil {
		return 3
	}
	return 2
}
