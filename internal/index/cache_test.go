package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestCacheIsScopedToItsStores(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	for _, home := range []string{"home-a", "home-b", "home-a"} {
		claude := filepath.Join(root, home, "claude")
		codex := filepath.Join(root, home, "codex")
		t.Setenv("CLAUDE_CONFIG_DIR", claude)
		t.Setenv("CODEX_HOME", codex)
		t.Setenv("CODEX_SQLITE_HOME", codex)
		for path, body := range map[string]string{
			filepath.Join(claude, "projects", "project", home+".jsonl"): `{"type":"user","message":{"role":"user","content":"` + home + `"}}`,
			filepath.Join(codex, "sessions", home+".jsonl"):             `{"type":"session_meta","payload":{"id":"` + home + `"}}` + "\n" + `{"type":"event_msg","payload":{"type":"user_message","message":"` + home + `"}}`,
		} {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		got, err := Load(provider.All)
		if err != nil || len(got) != 2 {
			t.Fatalf("relocated stores = %+v, %v", got, err)
		}
		for _, entry := range got {
			if entry.SessionID != home || entry.FirstPrompt != home {
				t.Fatalf("another store's session returned: %+v", entry)
			}
		}
	}
}

func TestLoadSeesSameSizeTranscriptReplacement(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	path := filepath.Join(root, "claude", "projects", "project", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"alpha"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(provider.All); err != nil || len(got) != 1 || got[0].FirstPrompt != "alpha" {
		t.Fatalf("initial prompt = %+v, %v", got, err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"bravo"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	changed := time.Now().Add(time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(provider.All); err != nil || len(got) != 1 || got[0].FirstPrompt != "bravo" {
		t.Fatalf("replacement prompt = %+v, %v", got, err)
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
	got, err := scanClaude(testIndex(t))
	if err != nil || len(got) != 1 || got[0].SessionID != "current" || got[0].FullPath != path || got[0].FirstPrompt != "current prompt" || got[0].Modified.Year() == 2020 {
		t.Fatalf("stale index reused: %v, err %v", got, err)
	}
}

func TestMovedClaudeStoreUsesItsLocalTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "codex"))
	dir := filepath.Join(root, "claude", "projects", "project")
	local := filepath.Join(dir, "moved.jsonl")
	original := filepath.Join(root, "original", "moved.jsonl")
	for path, prompt := range map[string]string{local: "local conversation", original: "old conversation"} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		body := `{"type":"user","cwd":"/work","message":{"role":"user","content":"` + prompt + `"}}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(original)
	if err != nil {
		t.Fatal(err)
	}
	idx := session.SessionsIndex{Entries: []session.SessionIndexEntry{{SessionID: "moved", FullPath: original, FirstPrompt: "old conversation", FileMtime: float64(info.ModTime().UnixMilli())}}}
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions-index.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load([]provider.Kind{provider.Claude})
	if err != nil || len(got) != 1 || got[0].FullPath != local || got[0].FirstPrompt != "local conversation" {
		t.Fatalf("moved store returned the wrong conversation: %+v, %v", got, err)
	}
}
