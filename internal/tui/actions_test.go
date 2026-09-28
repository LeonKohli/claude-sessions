package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestActionsOpenRecordedFilesFromActiveSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	body := `{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/work/proof.go":{"type":"add","content":"package evidence"}}}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	m := press(t, sized(NewModel([]session.SessionEntry{{Provider: provider.Codex, SessionID: "one", Summary: "shared session", FullPath: path}})), "/")
	m = press(t, m, "shared")
	next, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = next.(Model)
	m = press(t, m, "recorded files")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		next, cmd = m.Update(cmd())
		m = next.(Model)
	}
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(Model)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "package evidence") {
		t.Fatal("action did not open recorded file content")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if m.searchInput.Value() != "shared" {
		t.Fatal("action search replaced the session query")
	}
}

func TestCancelActionsPreservesSearchInput(t *testing.T) {
	m := press(t, sized(NewModel(fixtures())), "/")
	m = press(t, m, "Audit")
	next, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = next.(Model)
	m = press(t, m, "no such action")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	m = press(t, m, " the")
	if m.searchInput.Value() != "Audit the" {
		t.Fatal("canceling actions lost search focus or changed its query")
	}
}

func TestPreviewEndDoesNotChangeSelectedSession(t *testing.T) {
	m := sized(NewModel(fixtures()))
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(Model)
	m = press(t, m, "G")
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(line, "▸") && strings.Contains(line, "Audit the repository") {
			return
		}
	}
	t.Fatal("preview navigation changed the selected session")
}
