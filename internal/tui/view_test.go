package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

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
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
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

func TestProviderFilterCycles(t *testing.T) {
	m := sized(NewModel(fixtures()))

	m = press(t, m, "f") // all -> claude
	if got := m.providerFilter.String(); got != "claude" {
		t.Fatalf("filter = %q, want claude", got)
	}
	for _, s := range m.filtered {
		if s.Provider != provider.Claude {
			t.Errorf("claude filter leaked a %s session", s.Provider)
		}
	}

	m = press(t, m, "f") // claude -> codex
	if got := m.providerFilter.String(); got != "codex" {
		t.Fatalf("filter = %q, want codex", got)
	}
	for _, s := range m.filtered {
		if s.Provider != provider.Codex {
			t.Errorf("codex filter leaked a %s session", s.Provider)
		}
	}

	m = press(t, m, "f") // codex -> all
	if m.providerFilter != providerAll || len(m.filtered) != 2 {
		t.Errorf("cycle did not return to all: %v, %d entries", m.providerFilter, len(m.filtered))
	}
}

func TestViewRendersBothProviders(t *testing.T) {
	m := sized(NewModel(fixtures()))
	out := m.View()

	for _, want := range []string{"cc", "cx", "Audit the repository", "Fix auth middleware", "Search:"} {
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
	if out := m.View(); !strings.Contains(out, "Wegener/explorer") {
		t.Error("revealed subagent is missing its agent label")
	}
}

// Resuming must dispatch to the agent that owns the session, from its own
// directory, and must refuse Claude subagent transcripts outright.
func TestResumeTargetsTheOwningAgent(t *testing.T) {
	m := press(t, sized(NewModel(fixtures())), "\r")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
		_ = m.View()
	}
}

func TestViewWithNoSessions(t *testing.T) {
	m := sized(NewModel(nil))
	if out := m.View(); !strings.Contains(out, "No sessions found") {
		t.Error("empty state not rendered")
	}
}
