package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestCodexMissingSelectedRolloutDoesNotReadOldHistory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CODEX_SQLITE_HOME", root)
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := `{"type":"session_meta","payload":{"id":"reverted","cwd":"/tmp"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"user_message","message":"obsolete conversation"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-old.jsonl"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE threads(id TEXT, rollout_path TEXT, cwd TEXT, created_at INTEGER, updated_at INTEGER, history_mode TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?)`, "reverted", filepath.Join(dir, "missing.jsonl"), "/tmp", 1, 2, "paginated"); err != nil {
		t.Fatal(err)
	}
	got, err := ScanCodex()
	if err == nil {
		t.Fatalf("missing selected history silently fell back: %+v", got)
	}
}

func TestCodexMissingAncestorDoesNotReadOldHistory(t *testing.T) {
	for _, database := range []bool{false, true} {
		t.Run(fmt.Sprintf("database=%v", database), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEX_HOME", root)
			t.Setenv("CODEX_SQLITE_HOME", root)
			dir := filepath.Join(root, "sessions")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			child := filepath.Join(dir, "selected.jsonl")
			for path, body := range map[string]string{
				child:                                `{"ordinal":2,"type":"session_meta","payload":{"id":"selected","history_mode":"paginated","history_base":{"thread_id":"00000000-0000-0000-0000-000000000001","end_ordinal_exclusive":2,"end_byte_offset":1000}}}`,
				filepath.Join(dir, "obsolete.jsonl"): "{\"type\":\"session_meta\",\"payload\":{\"id\":\"selected\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"obsolete history\"}}",
			} {
				if err := os.WriteFile(path, []byte(body+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if database {
				db, err := sql.Open("sqlite", filepath.Join(root, "state_5.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec(`CREATE TABLE threads(id TEXT, rollout_path TEXT, cwd TEXT, created_at INTEGER, updated_at INTEGER, history_mode TEXT)`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?)`, "selected", child, "/work", 1, 2, "paginated"); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := ScanCodex()
			if !errors.Is(err, session.ErrHistoryUnavailable) {
				t.Fatalf("unavailable history became entries=%+v err=%v", entries, err)
			}
		})
	}
}

func TestCodexIndexesInheritedPromptWithChildIdentity(t *testing.T) {
	for _, database := range []bool{false, true} {
		t.Run(fmt.Sprintf("database=%v", database), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEX_HOME", root)
			t.Setenv("CODEX_SQLITE_HOME", root)
			dir := filepath.Join(root, "sessions")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			const parentID = "00000000-0000-0000-0000-000000000001"
			parent := filepath.Join(dir, "rollout-2026-09-26T00-00-00-"+parentID+".jsonl")
			prefix := `{"ordinal":0,"type":"session_meta","payload":{"id":"parent","history_mode":"paginated"}}` + "\n" +
				`{"ordinal":1,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"inherited user prompt"}]}}` + "\n"
			if err := os.WriteFile(parent, []byte(prefix), 0600); err != nil {
				t.Fatal(err)
			}
			child := filepath.Join(dir, "rollout-child.jsonl")
			body := fmt.Sprintf(`{"ordinal":2,"type":"session_meta","payload":{"id":"child","cwd":"/work/child","history_mode":"paginated","history_base":{"thread_id":%q,"end_ordinal_exclusive":2,"end_byte_offset":%d}}}`+"\n", parentID, len(prefix))
			if err := os.WriteFile(child, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if database {
				db, err := sql.Open("sqlite", filepath.Join(root, "state_5.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec(`CREATE TABLE threads(id TEXT, rollout_path TEXT, cwd TEXT, created_at INTEGER, updated_at INTEGER, history_mode TEXT)`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?)`, "child", child, "/work/child", 1, 2, "paginated"); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := ScanCodex()
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.SessionID != "child" {
					continue
				}
				if entry.FullPath != child || entry.ProjectPath != "/work/child" || entry.FirstPrompt != "inherited user prompt" {
					t.Fatalf("child identity or prompt changed: %+v", entry)
				}
				if database && len(entries) != 1 {
					t.Fatalf("database selection replaced by rollouts: %+v", entries)
				}
				return
			}
			t.Fatalf("child missing from index: %+v", entries)
		})
	}
}

func TestSearchSeesNewAmbiguityInHistoryArchive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const parentID = "00000000-0000-0000-0000-000000000001"
	name := "rollout-2026-09-26T00-00-00-" + parentID + ".jsonl"
	prefix := `{"ordinal":0,"type":"session_meta","payload":{"history_mode":"paginated"}}` + "\n" +
		`{"ordinal":1,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"needle in inherited prompt"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(prefix), 0600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "child.jsonl")
	body := fmt.Sprintf(`{"ordinal":2,"type":"session_meta","payload":{"history_mode":"paginated","history_base":{"thread_id":%q,"end_ordinal_exclusive":2,"end_byte_offset":%d}}}`+"\n", parentID, len(prefix))
	if err := os.WriteFile(child, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	entries := []session.SessionEntry{{Provider: provider.Codex, SessionID: "selected-child", FullPath: child}}
	hits, err := SearchSessions(context.Background(), entries, "needle", 1, 80)
	if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "selected-child" || hits[0].Matches != 1 {
		t.Fatalf("initial search = %+v, error = %v", hits, err)
	}
	archive := filepath.Join(root, "archived_sessions")
	if err := os.MkdirAll(archive, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, name), []byte(prefix), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SearchSessions(context.Background(), entries, "needle", 1, 80); !errors.Is(err, session.ErrHistoryUnavailable) {
		t.Fatalf("new ambiguous ancestor ignored: %v", err)
	}
}
