// Package provider identifies which coding agent produced a session and
// locates that agent's on-disk transcript store.
package provider

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Kind is a coding agent whose sessions this tool can browse.
type Kind uint8

const (
	Claude Kind = iota
	Codex
)

// All lists every supported provider in display order.
var All = []Kind{Claude, Codex}

func (k Kind) String() string {
	switch k {
	case Codex:
		return "codex"
	default:
		return "claude"
	}
}

// ResumeArgv returns the binary name and argv that reopen a session.
// Both agents accept the session UUID directly.
func (k Kind) ResumeArgv(sessionID string) (bin string, argv []string) {
	switch k {
	case Codex:
		return "codex", []string{"codex", "resume", sessionID}
	default:
		return "claude", []string{"claude", "--resume", sessionID}
	}
}

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// ClaudeHome honours CLAUDE_CONFIG_DIR, matching Claude Code itself.
func ClaudeHome() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(userHome(), ".claude")
}

// CodexHome honours CODEX_HOME, matching the Codex CLI.
func CodexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(userHome(), ".codex")
}

func ClaudeProjectsDir() string { return filepath.Join(ClaudeHome(), "projects") }
func CodexSessionsDir() string  { return filepath.Join(CodexHome(), "sessions") }
func CodexArchivedDir() string  { return filepath.Join(CodexHome(), "archived_sessions") }

// CodexStateDB returns the newest state_<n>.sqlite in the Codex home, or ""
// when none exists. The numeric suffix bumps on every schema migration, so the
// highest one is the live database.
func CodexStateDB() string {
	home := os.Getenv("CODEX_SQLITE_HOME")
	if home == "" {
		home = CodexHome()
	}
	matches, err := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
	if err != nil {
		return ""
	}
	best, bestVer := "", -1
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), ".sqlite")
		ver, err := strconv.Atoi(strings.TrimPrefix(name, "state_"))
		if err != nil || ver <= bestVer {
			continue
		}
		best, bestVer = m, ver
	}
	return best
}
