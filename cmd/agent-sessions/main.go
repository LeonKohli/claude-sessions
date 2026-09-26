package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeonKohli/claude-sessions/internal/cli"
	"github.com/LeonKohli/claude-sessions/internal/index"
	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/tui"
)

func main() {
	// Subcommands and piped invocations are handled non-interactively; only a
	// bare call from a terminal reaches the browser below.
	exit, runTUI := cli.Run(os.Args[1:])
	if !runTUI {
		os.Exit(exit)
	}
	os.Exit(runBrowser())
}

func runBrowser() int {
	claudeOnly := flag.Bool("claude", false, "show only Claude Code sessions")
	codexOnly := flag.Bool("codex", false, "show only Codex sessions")
	flag.Parse()

	kinds := provider.All
	switch {
	case *claudeOnly && !*codexOnly:
		kinds = []provider.Kind{provider.Claude}
	case *codexOnly && !*claudeOnly:
		kinds = []provider.Kind{provider.Codex}
	}

	sessions, err := index.Load(kinds)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: loading sessions: %v\n", err)
		return 1
	}
	if len(sessions) == 0 {
		fmt.Fprintf(os.Stderr, "error: no sessions found. Checked %s and %s\n",
			provider.ClaudeProjectsDir(), provider.CodexSessionsDir())
		return 1
	}

	p := tea.NewProgram(tui.NewModel(sessions), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Post-quit: if the user selected a session, exec into its agent.
	if fm, ok := finalModel.(tui.Model); ok {
		fm.ExecResume()
	}
	return 0
}
