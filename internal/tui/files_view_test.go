package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestBrowseRecordedFileAndReturnToSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	body := `{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/work/main.go":{"type":"add","content":"package restored\n"},"/work/other.go":{"type":"update","unified_diff":"@@\n-old\n+new"}}}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	m := sized(NewModel([]session.SessionEntry{{Provider: provider.Codex, SessionID: "files", FullPath: path, FirstPrompt: "restore code"}}))
	m.searchInput.SetValue("restore")
	m.applyFilters()
	next, cmd := m.Update(tea.KeyPressMsg{Text: "o"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("opening recorded files did nothing")
	}
	next, cmd = m.Update(cmd())
	m = next.(Model)
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(Model)
	}
	if view := m.View().Content; !strings.Contains(view, "package restored") || !strings.Contains(view, "Historical content") {
		t.Fatalf("recorded bytes not displayed:\n%s", view)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(Model)
	}
	if view := m.View().Content; !strings.Contains(view, "+new") || !strings.Contains(view, "Recorded diff") {
		t.Fatalf("diff not displayed:\n%s", view)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.resumeSessionID != "" {
		t.Fatal("Enter in files resumed a session")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if m.searchInput.Value() != "restore" || !strings.Contains(m.View().Content, "restore code") {
		t.Fatal("return lost search context")
	}
}

func TestTerminalViewsFitWindow(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, height := range []int{12, 24, 32} {
			m := NewModel(fixtures())
			next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m = next.(Model)
			for _, files := range []bool{false, true} {
				if files {
					m.files = &fileBrowser{entry: fixtures()[0], files: []session.FileChange{{Path: strings.Repeat("long/", 30) + "main.go", Recoverable: true}}, text: strings.Repeat("line\n", 100)}
				}
				view := m.View().Content
				if lipgloss.Width(view) > width || lipgloss.Height(view) > height {
					t.Errorf("%dx%d files=%v produced %dx%d", width, height, files, lipgloss.Width(view), lipgloss.Height(view))
				}
			}
		}
	}
}

func TestMainViewSummarizesRecordedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	body := `{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/work/new.go":{"type":"add","content":"new"},"/work/edit.go":{"type":"update","unified_diff":"-old\n+new"},"/work/old.go":{"type":"delete","content":"old"}}}}
{"type":"event_msg","payload":{"type":"patch_apply_end","success":false,"changes":{"/work/failed.go":{"type":"add","content":"failed"}}}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	next, load := NewModel([]session.SessionEntry{{Provider: provider.Codex, SessionID: "changes", FullPath: path, Summary: "Change files"}}).Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	next, _ = next.(Model).Update(load())
	view := next.(Model).View().Content
	for _, want := range []string{"Recorded files: 3", "2 with saved content", "1 added", "1 updated", "1 deleted"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in main view", want)
		}
	}
}

func TestReopenedFilesRejectEarlierContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write := func(text string) {
		t.Helper()
		body := `{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/work/main.go":{"type":"add","content":"` + text + `"}}}}`
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old bytes")
	m := sized(NewModel([]session.SessionEntry{{Provider: provider.Codex, SessionID: "one", FullPath: path, Summary: "files"}}))
	next, load := m.Update(tea.KeyPressMsg{Text: "o"})
	next, content := next.(Model).Update(load())
	m = next.(Model)
	old := content()
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	write("fresh bytes")
	next, load = m.Update(tea.KeyPressMsg{Text: "o"})
	m = finishCommands(next.(Model), load)
	next, cmd := m.Update(old)
	m = finishCommands(next.(Model), cmd)
	if view := m.View().Content; !strings.Contains(view, "fresh bytes") || strings.Contains(view, "old bytes") {
		t.Fatalf("old file response replaced current bytes:\n%s", view)
	}
}
