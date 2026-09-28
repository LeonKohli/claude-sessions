package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// The cache lives at one fixed path but the stores it indexes are relocatable
// via CLAUDE_CONFIG_DIR and CODEX_HOME. Pointing at different homes must not
// serve the previous homes' sessions.
func TestCacheIsScopedToItsStores(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/home-a/.claude")
	t.Setenv("CODEX_HOME", "/tmp/home-a/.codex")

	fixture := []session.SessionEntry{{
		Provider: provider.Codex, SessionID: "abc", FullPath: "/tmp/a.jsonl", Summary: "hi",
	}}
	if err := SaveCache(fixture, time.Now()); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	if got := LoadCache(); got == nil || !reflect.DeepEqual(got.Sessions, fixture) {
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

func TestCacheRejectsChangesAfterScanStarted(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	path := filepath.Join(root, "claude", "projects", "project", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	cache := &CachedIndex{ScannedAt: started, Sessions: []session.SessionEntry{{FullPath: path, FileSize: 3}}}
	if !IsCacheValid(cache) {
		t.Fatal("unchanged store rejected")
	}
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := started.Add(time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	if IsCacheValid(cache) {
		t.Fatal("same-size change after scan was accepted as current")
	}
}

func TestCacheDiscoversPreviouslyUnlistedTranscripts(t *testing.T) {
	for _, relative := range []string{"claude/projects/project/empty.jsonl", "claude/projects/project/parent/subagents/agent-new.jsonl", "codex/archived_sessions/new.jsonl"} {
		t.Run(relative, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
			t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
			path := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if got, err := Load(provider.All); err != nil || len(got) != 0 {
				t.Fatalf("empty transcript produced sessions: %v, err %v", got, err)
			}
			body := `{"type":"user","cwd":"/project","message":{"role":"user","content":"new conversation"}}` + "\n"
			if relative == "codex/archived_sessions/new.jsonl" {
				body = `{"type":"session_meta","payload":{"id":"new","cwd":"/project"}}` + "\n" + `{"type":"event_msg","payload":{"type":"user_message","message":"new conversation"}}` + "\n"
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			future := time.Now().Add(time.Second)
			if err := os.Chtimes(path, future, future); err != nil {
				t.Fatal(err)
			}
			got, err := Load(provider.All)
			if err != nil || len(got) != 1 || got[0].FullPath != path || got[0].FirstPrompt != "new conversation" {
				t.Fatalf("new conversation absent: %v, err %v", got, err)
			}
		})
	}
}

func TestClaudeIndexDoesNotOverrideNewerTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "current.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/project","message":{"role":"user","content":"current prompt"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	idx := session.SessionsIndex{Entries: []session.SessionIndexEntry{{SessionID: "current", FullPath: path, FirstPrompt: "stale prompt", Modified: "2020-01-01T00:00:00Z", FileMtime: 1}}}
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions-index.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ScanClaude()
	if err != nil || len(got) != 1 || got[0].SessionID != "current" || got[0].FullPath != path || got[0].FirstPrompt != "current prompt" || got[0].Modified.Year() == 2020 {
		t.Fatalf("stale index reused: %v, err %v", got, err)
	}
}
