package index

import (
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// The cache lives at one fixed path but the stores it indexes are relocatable
// via CLAUDE_CONFIG_DIR and CODEX_HOME. Pointing at different homes must not
// serve the previous homes' sessions.
func TestCacheIsScopedToItsStores(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep the real cache out of this test
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/home-a/.claude")
	t.Setenv("CODEX_HOME", "/tmp/home-a/.codex")

	fixture := []session.SessionEntry{{
		Provider: provider.Codex, SessionID: "abc", FullPath: "/tmp/a.jsonl", Summary: "hi",
	}}
	if err := SaveCache(fixture); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	if got := LoadCache(); got == nil || len(got.Sessions) != 1 {
		t.Fatalf("cache did not round-trip for the stores that wrote it: %+v", got)
	}

	t.Setenv("CODEX_HOME", "/tmp/home-b/.codex")
	if got := LoadCache(); got != nil {
		t.Errorf("cache from a different CODEX_HOME was reused: %d sessions", len(got.Sessions))
	}

	t.Setenv("CODEX_HOME", "/tmp/home-a/.codex")
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/home-b/.claude")
	if got := LoadCache(); got != nil {
		t.Errorf("cache from a different CLAUDE_CONFIG_DIR was reused: %d sessions", len(got.Sessions))
	}
}
