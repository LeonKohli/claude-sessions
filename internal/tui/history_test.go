package tui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestLateRolloutPreviewDoesNotReplaceSelectedHistory(t *testing.T) {
	const thread = "01a0dac5-f897-7121-b3ef-5e0ccc0c75ea"
	var entries []session.SessionEntry
	for i, id := range []string{"01a0dac8-0d70-78e3-baf1-01b02d9bd319", "01a0dcdc-e347-7421-8a61-4ee973b5e1a3"} {
		path := filepath.Join(t.TempDir(), "rollout-2026-09-26T00-00-00-"+thread+"_"+id+".jsonl")
		text := []string{"first rollout text", "second rollout text"}[i]
		body := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, session.SessionEntry{Provider: provider.Codex, SessionID: thread, FullPath: path, FirstPrompt: "shared title", Modified: time.Unix(int64(2-i), 0)})
	}
	m := NewModel(entries)
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	stale := cmd
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("switching physical rollouts reused the old preview")
	}
	m = finishCommands(m, cmd)
	m = finishCommands(m, stale)
	if !strings.Contains(m.View().Content, "second rollout text") || strings.Contains(m.View().Content, "first rollout text") {
		t.Fatalf("wrong preview:\n%s", m.View().Content)
	}
}

func TestReturningToSessionRejectsEarlierPreview(t *testing.T) {
	for _, delayedRender := range []bool{false, true} {
		t.Run(fmt.Sprint(delayedRender), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conversation.jsonl")
			write := func(text string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"`+text+`"}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("old preview")
			next, load := NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path, Modified: time.Unix(2, 0)}, {SessionID: "two", Summary: "other", FullPath: path, Modified: time.Unix(1, 0)}}).Update(tea.WindowSizeMsg{Width: 120, Height: 35})
			m := next.(Model)
			old := load()
			if delayedRender {
				var render tea.Cmd
				next, render = m.Update(old)
				m = next.(Model)
				old = render()
			}
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m = next.(Model)
			write("fresh preview")
			next, load = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
			m = finishCommands(next.(Model), load)
			next, cmd := m.Update(old)
			m = finishCommands(next.(Model), cmd)
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "fresh preview") || strings.Contains(view, "old preview") {
				t.Fatalf("earlier preview replaced current result:\n%s", view)
			}
		})
	}
}
