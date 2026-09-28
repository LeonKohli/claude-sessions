package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestReaderFormatsAssistantMarkdownButKeepsPromptLiteral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	var body strings.Builder
	for _, msg := range []struct{ role, text string }{
		{"user", "Keep **literal** in my prompt"},
		{"assistant", "## Result\n\nA **formatted phrase** with `code`.\n\n```go\n    return true\n```"},
	} {
		line, err := json.Marshal(map[string]any{"type": msg.role, "message": map[string]string{"role": msg.role, "content": msg.text}})
		if err != nil {
			t.Fatal(err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	m := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "Markdown", FullPath: path}}))
	next, load := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = finishCommands(next.(Model), load)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "**literal**") || strings.Contains(view, "**formatted phrase**") || strings.Contains(view, "```go") || !strings.Contains(view, "return true") {
		t.Fatalf("prompt and Markdown answer were not rendered distinctly:\n%s", view)
	}
	m = press(t, m, "/")
	m = press(t, m, "formatted phrase")
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "1 match") || !strings.Contains(view, "formatted phrase") {
		t.Fatalf("formatted text could not be found:\n%s", view)
	}
}

func TestReadFindAndReturnPreservesSearchSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	var body strings.Builder
	for i := 0; i < 45; i++ {
		fmt.Fprintf(&body, `{"type":"user","message":{"role":"user","content":"turn %d"}}`+"\n", i)
	}
	body.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"final needle\n    indented code"}}` + "\n")
	if err := os.WriteFile(path, []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	entries := []session.SessionEntry{{SessionID: "one", Summary: "shared first", FullPath: path}, {SessionID: "two", Summary: "shared second", FullPath: path}}
	m := press(t, sized(NewModel(entries)), "/")
	m = press(t, m, "shared")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.resumeSessionID != "" {
		t.Fatal("reading unexpectedly resumed the agent")
	}
	if cmd != nil {
		m = finishCommands(m, cmd)
	}
	m = press(t, m, "/")
	m = press(t, m, "needle")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	// Source mode preserves indentation that Markdown treats as paragraph whitespace.
	next, cmd = m.Update(tea.KeyPressMsg{Text: "m"})
	m = finishCommands(next.(Model), cmd)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "final needle") || !strings.Contains(view, "    indented code") {
		t.Fatalf("reader did not find text beyond preview or preserve code:\n%s", view)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if m.searchInput.Value() != "shared" || m.filtered[m.cursor].SessionID != "two" {
		t.Fatal("reading lost the selected search result")
	}
}

func TestPastedSearchTextIsNotAKeyboardCommand(t *testing.T) {
	m := press(t, sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "enter the project"}})), "/")
	next, _ := m.Update(tea.PasteMsg{Content: "enter"})
	m = next.(Model)
	if m.searchInput.Value() != "enter" || !strings.Contains(ansi.Strip(m.View().Content), "enter the project") {
		t.Fatal("pasted query was treated as a key command")
	}
}

func TestReaderFindsPhraseAcrossWrappedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"` + strings.Repeat("a", 29) + ` cross boundary"}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	next, _ := NewModel([]session.SessionEntry{{SessionID: "one", Summary: "phrase", FullPath: path}}).Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	next, load := next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m := press(t, finishCommands(next.(Model), load), "/")
	m = press(t, m, "cross boundary")
	if !strings.Contains(ansi.Strip(m.View().Content), "1 match") {
		t.Fatal("word wrapping broke phrase search")
	}
}

func finishCommands(m Model, cmd tea.Cmd) Model {
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				m = finishCommands(m, child)
			}
			return m
		}
		next, nextCmd := m.Update(msg)
		m, cmd = next.(Model), nextCmd
	}
	return m
}

func TestReopenedReaderIgnoresEarlierLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"`+text+`"}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old content")
	m := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path}}))
	next, load := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	old := load()
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	write("fresh content")
	next, load = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = finishCommands(next.(Model), load)
	next, cmd := m.Update(old)
	m = finishCommands(next.(Model), cmd)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "fresh content") || strings.Contains(view, "old content") {
		t.Fatalf("old load replaced reopened conversation:\n%s", view)
	}
}

func TestReaderCanCloseBeforeFormattingFinishes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"assistant","message":{"role":"assistant","content":"**answer**"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path}}))
	next, load := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, render := next.(Model).Update(load())
	m = next.(Model)
	if render == nil || !strings.Contains(m.View().Content, "Loading conversation") {
		t.Fatal("formatting did not yield while the reader remained interactive")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	m = finishCommands(m, render)
	if m.reader != nil || !strings.Contains(m.View().Content, "Sessions") {
		t.Fatal("finished formatting reopened a closed reader")
	}
}

func TestReturningFromSourceShowsMarkdownWithoutLoadingAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"assistant","message":{"role":"assistant","content":"A **formatted phrase**."}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path}}))
	next, load := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = finishCommands(next.(Model), load)
	next, render := m.Update(tea.KeyPressMsg{Text: "m"})
	m = finishCommands(next.(Model), render)
	if !strings.Contains(ansi.Strip(m.View().Content), "**formatted phrase**") {
		t.Fatal("source mode lost the original Markdown")
	}
	next, _ = m.Update(tea.KeyPressMsg{Text: "m"})
	m = next.(Model)
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "Loading conversation") || !strings.Contains(view, "formatted phrase") || strings.Contains(view, "**formatted phrase**") {
		t.Fatalf("returning to unchanged Markdown required another load:\n%s", view)
	}
	m = press(t, m, "/")
	m = press(t, m, "formatted phrase")
	if !strings.Contains(ansi.Strip(m.View().Content), "1 match") {
		t.Fatal("reused Markdown could not be searched")
	}
}

func TestReturningFromSourceUsesCurrentWidthAndTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"assistant","message":{"role":"assistant","content":"A **formatted phrase** long enough to wrap across narrow terminal lines."}}`), 0600); err != nil {
		t.Fatal(err)
	}
	open := func() Model {
		m := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path}}))
		next, load := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return finishCommands(next.(Model), load)
	}
	for name, change := range map[string]tea.Msg{
		"width": tea.WindowSizeMsg{Width: 40, Height: 24},
		"theme": tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")},
	} {
		t.Run(name, func(t *testing.T) {
			m := open()
			next, render := m.Update(tea.KeyPressMsg{Text: "m"})
			m = finishCommands(next.(Model), render)
			next, render = m.Update(change)
			m = finishCommands(next.(Model), render)
			next, render = m.Update(tea.KeyPressMsg{Text: "m"})
			m = finishCommands(next.(Model), render)

			fresh := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path}}))
			next, render = fresh.Update(change)
			fresh = finishCommands(next.(Model), render)
			next, load := fresh.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			fresh = finishCommands(next.(Model), load)
			if m.View().Content != fresh.View().Content {
				t.Fatal("returning from source used stale Markdown layout or colors")
			}
		})
	}
}

func TestReaderKeepsLatestWidthWhenRenderFinishesOutOfOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"A long paragraph with enough words to wrap across several narrow lines correctly."}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "conversation", FullPath: path}}))
	next, load := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, oldRender := next.(Model).Update(load())
	next, newRender := next.(Model).Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = finishCommands(next.(Model), newRender)
	m = finishCommands(m, oldRender)
	m = press(t, m, "/")
	m = press(t, m, "narrow lines")
	if !strings.Contains(ansi.Strip(m.View().Content), "1 match") {
		t.Fatal("old render width replaced the resized conversation")
	}
	if lipgloss.Width(m.reader.viewport.View()) > 36 {
		t.Fatal("reader content exceeds resized viewport")
	}
}

func TestResizeBurstDoesNotStartConcurrentConversationRenders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"assistant","message":{"role":"assistant","content":"A **visible answer** after resizing."}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []bool{false, true} {
		t.Run(fmt.Sprintf("reader=%v", reader), func(t *testing.T) {
			next, load := NewModel([]session.SessionEntry{{SessionID: "one", FullPath: path}}).Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			if reader {
				next, load = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			}
			next, pending := next.(Model).Update(load())
			m := next.(Model)
			for _, width := range []int{100, 90, 80, 60} {
				next, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
				m = next.(Model)
				// Formatting large transcripts must have bounded in-flight work.
				if cmd != nil {
					t.Fatal("resizing launched more work while formatting was unfinished")
				}
			}
			m = finishCommands(m, pending)
			view := m.View().Content
			if !strings.Contains(ansi.Strip(view), "visible answer") || lipgloss.Width(view) > 60 {
				t.Fatal("finishing the pending render did not display the latest layout")
			}
		})
	}
}

func TestStartupPreviewDoesNotOverwriteNewerPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"`+s+`"}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old startup content")
	m := NewModel([]session.SessionEntry{{SessionID: "one", FullPath: path}})
	var startup []tea.Msg
	first := m.Init()()
	if batch, ok := first.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			startup = append(startup, cmd())
		}
	} else {
		startup = append(startup, first)
	}
	write("fresh content")
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = finishCommands(next.(Model), cmd)
	for _, stale := range startup {
		next, cmd = m.Update(stale)
		m = finishCommands(next.(Model), cmd)
	}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "old startup content") || !strings.Contains(view, "fresh content") {
		t.Fatalf("startup result replaced newer content:\n%s", view)
	}
}

func TestPastingInFilesPreservesUnderlyingSearch(t *testing.T) {
	m := press(t, sized(NewModel([]session.SessionEntry{{SessionID: "one", Summary: "shared session"}})), "/")
	m = press(t, m, "shared")
	next, _ := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	m = press(t, next.(Model), "recorded files")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, _ = next.(Model).Update(tea.PasteMsg{Content: "unexpected"})
	next, _ = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	view := ansi.Strip(next.(Model).View().Content)
	if !strings.Contains(view, "shared session") || strings.Contains(view, "sharedunexpected") {
		t.Fatalf("paste changed hidden session search:\n%s", view)
	}
}
