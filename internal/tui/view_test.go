package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestSelectionStaysVisibleAfterScrollingAndResizing(t *testing.T) {
	var entries []session.SessionEntry
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("session-%d", i)
		entries = append(entries, session.SessionEntry{SessionID: id, ShortID: id, FirstPrompt: id, Modified: time.Unix(int64(100-i), 0)})
	}
	next, _ := NewModel(entries).Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m := next.(Model)
	for i := 0; i < 5; i++ {
		m = press(t, m, "j")
	}
	for _, height := range []int{24, 18} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: height})
		m = next.(Model)
		visible := false
		for _, line := range strings.Split(m.View().Content, "\n") {
			if strings.Contains(line, "▸") && strings.Contains(line, "session-5") {
				visible = true
			}
		}
		if !visible {
			t.Fatalf("selected session disappeared at height %d:\n%s", height, m.View().Content)
		}
	}
}

func TestPreviewReportsMissingTranscript(t *testing.T) {
	entry := session.SessionEntry{SessionID: "missing", FirstPrompt: "missing transcript", FullPath: filepath.Join(t.TempDir(), "missing.jsonl")}
	next, load := NewModel([]session.SessionEntry{entry}).Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m := next.(Model)
	if !strings.Contains(m.View().Content, "Loading") {
		t.Fatal("pending preview was shown as empty")
	}
	if load == nil {
		t.Fatal("preview did not start")
	}
	next, _ = m.Update(load())
	view := next.(Model).View().Content
	if !strings.Contains(view, "missing.jsonl") || !strings.Contains(view, "Preview unavailable") {
		t.Fatalf("missing transcript was shown as empty:\n%s", view)
	}
}

func TestPreviewUpdatesDisplayedUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"question"}}
{"type":"assistant","message":{"id":"one","role":"assistant","usage":{"input_tokens":3,"output_tokens":11},"content":[{"type":"thinking","thinking":"reasoning"}]}}
{"type":"assistant","message":{"id":"one","role":"assistant","usage":{"input_tokens":3,"output_tokens":11},"content":[{"type":"text","text":"answer"}]}}
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	entry := session.SessionEntry{SessionID: "usage", FirstPrompt: "usage", FullPath: path, FileSize: 3000}
	next, load := NewModel([]session.SessionEntry{entry}).Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	m := next.(Model)
	if load == nil {
		t.Fatal("preview did not start")
	}
	next, _ = m.Update(load())
	view := next.(Model).View().Content
	if !strings.Contains(view, "2 messages") || !strings.Contains(view, "14t") {
		t.Fatalf("display kept estimated counts after loading:\n%s", view)
	}
}

func TestCompletedEmptyTranscriptReplacesEstimatedMessageCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"summary\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := session.SessionEntry{SessionID: "empty", Summary: "Empty conversation", FullPath: path, FileSize: 90000, MessageCount: 8}
	next, load := NewModel([]session.SessionEntry{entry}).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m := finishCommands(next.(Model), load)
	view := m.View().Content
	if !strings.Contains(view, "0 messages") || strings.Contains(view, "8 messages") || strings.Contains(view, "~30 messages") {
		t.Fatalf("completed scan retained an old count or estimate:\n%s", view)
	}
}

func fixtures() []session.SessionEntry {
	now := time.Now()
	return []session.SessionEntry{
		{
			Provider: provider.Codex, SessionID: "019fa425-7964-7041-b895-b331deb81e89",
			ShortID: "019fa425", FullPath: "/tmp/a.jsonl", ProjectPath: "/tmp/motion",
			Summary: "Audit the repository", GitBranch: "codex/motion", Model: "gpt-5.6-sol",
			Created: now.Add(-2 * time.Hour), Modified: now.Add(-time.Hour), FileSize: 4096,
		},
		{
			Provider: provider.Claude, SessionID: "21ce0506-6762-4a1b-9f3c-0d5e7a8b1c2d",
			ShortID: "21ce0506", FullPath: "/tmp/b.jsonl", ProjectPath: "/tmp/api",
			Summary: "Fix auth middleware", GitBranch: "main", Model: "opus-4.6",
			Created: now.Add(-3 * time.Hour), Modified: now.Add(-2 * time.Hour), FileSize: 8192,
		},
		{
			Provider: provider.Codex, SessionID: "019f0945-f8c2-7882-96c7-b724df4839c2",
			ShortID: "019f0945", FullPath: "/tmp/c.jsonl", ProjectPath: "/tmp/studium",
			Summary: "Trace entry points", IsSubagent: true, AgentLabel: "Wegener/explorer",
			Created: now.Add(-5 * time.Hour), Modified: now.Add(-4 * time.Hour), FileSize: 2048,
		},
		{
			Provider: provider.Claude, SessionID: "agent-7f3c0d5e",
			ShortID: "agent-7f", FullPath: "/tmp/d.jsonl", ProjectPath: "/tmp/api",
			Summary: "search the codebase", IsSubagent: true,
			Created: now.Add(-6 * time.Hour), Modified: now.Add(-5 * time.Hour), FileSize: 1024,
		},
	}
}

func sized(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model)
}

func press(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyPressMsg{Text: key})
	return next.(Model)
}

// Subagent threads are indexed but stay out of the default list.
func TestSubagentsHiddenUntilToggled(t *testing.T) {
	m := sized(NewModel(fixtures()))
	if len(m.filtered) != 2 {
		t.Fatalf("default view shows %d sessions, want the 2 top-level ones", len(m.filtered))
	}

	m = press(t, m, "a")
	if len(m.filtered) != 4 {
		t.Fatalf("after toggling: %d sessions, want all 4", len(m.filtered))
	}

	m = press(t, m, "a")
	if len(m.filtered) != 2 {
		t.Fatalf("toggling back: %d sessions, want 2", len(m.filtered))
	}
}

func TestProviderFilterChoosesAgentByName(t *testing.T) {
	m := sized(NewModel(fixtures()))
	for _, choice := range []string{"claude", "codex", "all"} {
		m = press(t, m, "f")
		m = press(t, m, choice)
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if m.providerFilter.String() != choice {
			t.Fatalf("selected %s, got %s", choice, m.providerFilter.String())
		}
		if choice == "all" {
			if len(m.filtered) != 2 {
				t.Fatal("clearing the filter did not return both sessions")
			}
			continue
		}
		if len(m.filtered) != 1 || m.filtered[0].Provider.String() != choice {
			t.Fatalf("%s filter returned the wrong sessions", choice)
		}
	}
}

func TestViewRendersBothProviders(t *testing.T) {
	m := sized(NewModel(fixtures()))
	out := m.View().Content

	for _, want := range []string{"Claude", "Codex", "Audit the repository", "Fix auth middleware", "Search:"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered view is missing %q", want)
		}
	}
	if strings.Contains(out, "Wegener") {
		t.Error("a subagent leaked into the default view")
	}
}

func TestViewRendersAgentLabel(t *testing.T) {
	m := press(t, sized(NewModel(fixtures())), "a")
	if out := m.View().Content; !strings.Contains(out, "Wegener/explorer") {
		t.Error("revealed subagent is missing its agent label")
	}
}

// Resuming must dispatch to the agent that owns the session, from its own
// directory, and must refuse Claude subagent transcripts outright.
func TestResumeTargetsTheOwningAgent(t *testing.T) {
	m := press(t, sized(NewModel(fixtures())), "\r")
	next, _ := m.Update(tea.KeyPressMsg{Text: "r"})
	m = next.(Model)

	if m.resumeProvider != provider.Codex {
		t.Fatalf("resumeProvider = %v, want codex for the newest session", m.resumeProvider)
	}
	if m.resumeProject != "/tmp/motion" {
		t.Errorf("resumeProject = %q", m.resumeProject)
	}

	bin, argv := m.resumeProvider.ResumeArgv(m.resumeSessionID)
	if bin != "codex" || strings.Join(argv, " ") != "codex resume "+m.resumeSessionID {
		t.Errorf("argv = %v", argv)
	}

	claudeBin, claudeArgv := provider.Claude.ResumeArgv("abc")
	if claudeBin != "claude" || strings.Join(claudeArgv, " ") != "claude --resume abc" {
		t.Errorf("claude argv = %v", claudeArgv)
	}
}

func TestClaudeSubagentIsNotResumable(t *testing.T) {
	m := press(t, sized(NewModel(fixtures())), "a")
	// Sorted newest-first, the Claude agent-* transcript is last.
	m.cursor = len(m.filtered) - 1
	if s := m.filtered[m.cursor]; s.Provider != provider.Claude || !s.IsSubagent {
		t.Fatalf("fixture ordering changed: cursor is on %+v", s)
	}

	next, cmd := m.Update(tea.KeyPressMsg{Text: "r"})
	m = next.(Model)
	if m.resumeSessionID != "" {
		t.Error("a Claude subagent transcript was accepted for resume")
	}
	if cmd == nil {
		t.Fatal("expected a toast explaining the refusal")
	}
	if toast, ok := cmd().(ToastMsg); !ok || !strings.Contains(toast.Text, "resumable") {
		t.Errorf("unexpected message: %#v", cmd())
	}
}

// Search covers the provider name and agent label, not just the prompt text.
func TestSearchMatchesProviderAndAgent(t *testing.T) {
	m := sized(NewModel(fixtures()))
	m.showSubagents = true

	m.searchInput.SetValue("wegener")
	m.applyFilters()
	if len(m.filtered) != 1 || m.filtered[0].AgentLabel != "Wegener/explorer" {
		t.Errorf("agent label search returned %d entries", len(m.filtered))
	}

	m.searchInput.SetValue("codex")
	m.applyFilters()
	for _, s := range m.filtered {
		if s.Provider != provider.Codex {
			t.Errorf("provider-name search matched a %s session", s.Provider)
		}
	}
}

// A terminal narrow enough to drive layout maths negative must not panic.
func TestViewSurvivesTinyTerminal(t *testing.T) {
	m := NewModel(fixtures())
	for _, size := range []tea.WindowSizeMsg{{Width: 20, Height: 6}, {Width: 40, Height: 10}, {Width: 200, Height: 60}} {
		next, _ := m.Update(size)
		m = next.(Model)
		m.showSubagents = true
		m.applyFilters()
		_ = m.View().Content
	}
}

func TestViewWithNoSessions(t *testing.T) {
	m := sized(NewModel(nil))
	if out := m.View().Content; !strings.Contains(out, "No sessions found") {
		t.Error("empty state not rendered")
	}
}

func TestDeepSearchRejectsStaleResultsAndRefreshesSelection(t *testing.T) {
	m := sized(NewModel(fixtures()))
	m.searchMode = searchDeep
	m.searchInput.SetValue("current")
	_ = m.runDeepSearch("current")
	id := m.debounceID
	m.scrollOffset = 5
	result := DeepSearchResultMsg{ID: id, Query: "current", Results: []DeepMatch{{Session: fixtures()[1]}}}
	next, cmd := m.Update(result)
	m = next.(Model)
	if cmd == nil || m.previewSessID != fixtures()[1].SessionID || m.scrollOffset != 0 {
		t.Fatal("accepted results did not refresh selection and preview")
	}
	result.ID--
	result.Results = []DeepMatch{{Session: fixtures()[0]}}
	next, _ = m.Update(result)
	m = next.(Model)
	if len(m.filtered) != 1 || m.filtered[0].SessionID != fixtures()[1].SessionID {
		t.Fatal("stale results replaced the current results")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	result.ID = id
	next, _ = m.Update(result)
	m = next.(Model)
	if m.searchInput.Value() != "" || len(m.filtered) != 2 {
		t.Fatal("search completion undid Escape")
	}
}

func TestControlCQuitsWhileSearching(t *testing.T) {
	m := sized(NewModel(fixtures()))
	m.searchActive = true
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+C ignored")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Ctrl+C did not quit")
	}
}

func TestDeepSearchDisplaysTranscriptMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	entries := fixtures()[:2]
	for i := range entries {
		entries[i].FullPath = filepath.Join(t.TempDir(), "session.jsonl")
		entries[i].Provider = provider.Claude
		body := `{"type":"user","message":{"role":"user","content":"ordinary conversation"}}` + "\n"
		if i == 1 {
			body = `{"type":"user","message":{"role":"user","content":"needle in the transcript"}}` + "\n"
		}
		if err := os.WriteFile(entries[i].FullPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := press(t, sized(NewModel(entries)), "/")
	m = press(t, m, "needle")
	next, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("switching to transcript search did not start a search")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	view := m.View().Content
	for _, want := range []string{"Fix auth middleware", "needle in the transcript"} {
		if !strings.Contains(view, want) {
			t.Errorf("completed transcript search did not display %q", want)
		}
	}
	if strings.Contains(view, "Audit the repository") {
		t.Error("completed transcript search retained the nonmatching session")
	}
}

func TestLongSearchKeepsTheEditedEndVisible(t *testing.T) {
	next, _ := NewModel(fixtures()).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := press(t, next.(Model), "/")
	height := lipgloss.Height(m.renderSearchBar())
	m = press(t, m, strings.Repeat("earlier ", 12)+"latest")
	if lipgloss.Height(m.renderSearchBar()) != height {
		t.Fatalf("long query expanded from %d to %d:\n%s", height, lipgloss.Height(m.renderSearchBar()), m.renderSearchBar())
	}
	if !strings.Contains(m.View().Content, "latest") {
		t.Fatalf("the query's edited end disappeared:\n%s", m.View().Content)
	}
}

func TestEmptySearchFitsShortWindow(t *testing.T) {
	next, _ := NewModel(fixtures()).Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m := press(t, next.(Model), "/")
	m = press(t, m, "no matching session")
	view := m.View().Content
	if lipgloss.Height(view) > 18 || lipgloss.Width(view) > 80 {
		t.Fatalf("empty search overflows terminal: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}
}

func TestMetadataCannotSendTerminalCommandsOrOverflowStatus(t *testing.T) {
	control := "\x1b]52;c;Y2xpcGJvYXJk\x07"
	entries := fixtures()[:1]
	entries[0].Model = "model" + control
	m := sized(NewModel(entries))
	m.previewLoading = false
	next, _ := m.Update(ToastMsg{Text: "failed " + control + strings.Repeat("long-path/", 30)})
	m = next.(Model)
	view := m.View().Content
	if strings.Contains(view, control) {
		t.Fatal("untrusted metadata emitted a terminal command")
	}
	if lipgloss.Width(m.renderSearchBar()) > m.width {
		t.Fatal("long error expanded the status bar beyond the terminal")
	}
}
