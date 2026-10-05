package index

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

// A nonempty database selects visible histories; the filesystem is the fallback.
func TestCodexDiscoveryUsesDatabaseSelectionOrFilesystemFallback(t *testing.T) {
	for _, database := range []bool{false, true} {
		t.Run(fmt.Sprintf("database=%t", database), func(t *testing.T) {
			setupCodexDiscoveryStore(t, database)
			unindexed := filepath.Join(provider.CodexSessionsDir(), "rollout-unindexed.jsonl")
			body := `{"type":"session_meta","payload":{"id":"unindexed","cwd":"/work/unindexed"}}
{"type":"event_msg","payload":{"type":"user_message","message":"unindexed history"}}`
			if err := os.WriteFile(unindexed, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			entries, err := scanCodex(testIndex(t))
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{"main": false, "child": true}
			if !database {
				want["unindexed"] = false
			}
			if len(entries) != len(want) {
				t.Fatalf("unexpected histories: %+v", entries)
			}
			for _, entry := range entries {
				subagent, exists := want[entry.SessionID]
				if !exists || entry.Provider != provider.Codex || entry.ProjectPath != "/work/"+entry.SessionID || entry.IsSubagent != subagent {
					t.Errorf("unexpected session metadata: %+v", entry)
				}
				delete(want, entry.SessionID)
			}
			if len(want) != 0 {
				t.Fatalf("missing histories: %v", want)
			}
		})
	}
}

// TestCodexIDFromFilename covers the fallback used when a rollout's
// session_meta is unreadable but the filename still carries the UUID.
func TestCodexIDFromFilename(t *testing.T) {
	cases := map[string]string{
		"/x/rollout-2026-07-27T17-15-47-019fa425-7964-7041-b895-b331deb81e89.jsonl": "019fa425-7964-7041-b895-b331deb81e89",
		"/x/rollout-2026-02-03T14-37-14-019c23b8-b81d-74d2-8fef-b5fc90a50219.jsonl": "019c23b8-b81d-74d2-8fef-b5fc90a50219",
	}
	for path, want := range cases {
		if got := codexIDFromFilename(path); got != want {
			t.Errorf("codexIDFromFilename(%q) = %q, want %q", path, got, want)
		}
	}
}

// The state database is a live file Codex writes to. A reader can transiently
// fail to open it mid-checkpoint, so discovery must survive its absence by
// falling back to the rollout files.
func TestCodexScanFallsBackWithoutStateDB(t *testing.T) {
	setupCodexDiscoveryStore(t, false)
	real, err := scanCodexRollouts(testIndex(t))
	if err != nil || len(real) == 0 {
		t.Fatalf("fixture rollouts were not discovered: %v", err)
	}

	// A home with transcripts but no state_<n>.sqlite. WalkDir does not follow
	// symlinks, so the fixtures are copied rather than linked.
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "07", "27")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, e := range real {
		if want == 3 {
			break
		}
		body, err := os.ReadFile(e.FullPath)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(day, filepath.Base(e.FullPath)), body, 0o644); err != nil {
			t.Fatal(err)
		}
		want++
	}

	t.Setenv("CODEX_HOME", home)
	if db := provider.CodexStateDB(); db != "" {
		t.Fatalf("fixture home unexpectedly has a state database: %s", db)
	}

	entries, err := scanCodex(testIndex(t))
	if err != nil {
		t.Fatalf("ScanCodex without a state db: %v", err)
	}
	if len(entries) != want {
		t.Errorf("fallback found %d sessions, want %d", len(entries), want)
	}
	for _, e := range entries {
		if e.SessionID == "" || e.ProjectPath == "" {
			t.Errorf("fallback entry missing id or cwd: %+v", e)
		}
		if e.Provider != provider.Codex {
			t.Errorf("fallback entry tagged as %s", e.Provider)
		}
	}
}

// TestCodexEntriesResolveOnDisk guards the ghost-row filter: every indexed
// session must still have a readable transcript.
func TestCodexEntriesResolveOnDisk(t *testing.T) {
	setupCodexDiscoveryStore(t, true)
	entries, err := scanCodex(testIndex(t))
	if err != nil {
		t.Fatalf("fixture store scan: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("found %d entries, want the two recorded threads", len(entries))
	}
	for _, e := range entries {
		if _, err := os.Stat(e.FullPath); err != nil {
			t.Errorf("indexed session %s points at unreadable %s", e.ShortID, e.FullPath)
		}
		if e.DisplayTitle() == "" {
			t.Errorf("session %s has no display title", e.ShortID)
		}
	}
}

func setupCodexDiscoveryStore(t *testing.T, database bool) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "store?name#fragment")
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CODEX_SQLITE_HOME", root)
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, id := range []string{"main", "child"} {
		source := `"cli"`
		if id == "child" {
			source = `{"subagent":{"thread_spawn":{"parent_thread_id":"main"}}}`
		}
		body := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"source":%s}}
{"type":"event_msg","payload":{"type":"user_message","message":%q}}
`, id, "/work/"+id, source, "prompt "+id)
		paths[id] = filepath.Join(dir, "rollout-"+id+".jsonl")
		if err := os.WriteFile(paths[id], []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !database {
		return
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.Join(root, "state_5.sqlite")}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads(id TEXT, rollout_path TEXT, cwd TEXT, created_at INTEGER, updated_at INTEGER, thread_source TEXT, extra_future_column TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"main", "child", "deleted"} {
		source := "cli"
		if id == "child" {
			source = "subagent"
		}
		path := paths[id]
		if id == "deleted" {
			path = filepath.Join(dir, "missing.jsonl")
		}
		if _, err := db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?,?)`, id, path, "/work/"+id, 1, 2, source, "future metadata"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadSeesCodexDatabaseChangesWithoutTranscriptChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	setupCodexDiscoveryStore(t, true)
	if got, err := Load([]provider.Kind{provider.Codex}); err != nil || len(got) != 2 {
		t.Fatalf("initial discovery = %+v, %v", got, err)
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: provider.CodexStateDB()}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE threads SET cwd = ?, updated_at = ? WHERE id = ?", "/moved/project", 42, "main"); err != nil {
		t.Fatal(err)
	}
	got, err := Load([]provider.Kind{provider.Codex})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range got {
		if entry.SessionID == "main" {
			if entry.ProjectPath != "/moved/project" || entry.Modified.Unix() != 42 || entry.FirstPrompt != "prompt main" {
				t.Fatalf("database metadata lost: %+v", entry)
			}
			return
		}
	}
	t.Fatalf("updated thread missing: %+v", got)
}
