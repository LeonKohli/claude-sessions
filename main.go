package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/leon/claude-sessions/internal/index"
	"github.com/leon/claude-sessions/internal/tui"
)

func main() {
	sessions, err := index.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sessions: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModel(sessions)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Post-quit: if user selected a session to resume, exec into claude
	if fm, ok := finalModel.(tui.Model); ok {
		fm.ExecResume()
	}
}
