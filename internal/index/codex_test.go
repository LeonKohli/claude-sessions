package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

// TestCodexScanPathsAgree runs both Codex discovery strategies over the real
// store and asserts they describe the same threads. The SQLite index is the
// fast path; the rollout walk is the fallback when it is missing or its schema
// has drifted, so the two must not disagree about what exists.
func TestCodexScanPathsAgree(t *testing.T) {
	if provider.CodexStateDB() == "" {
		t.Skip("no codex state database on this machine")
	}

	fromDB, err := scanCodexStateDB()
	if err != nil {
		t.Fatalf("state db scan: %v", err)
	}
	fromFiles, err := scanCodexRollouts()
	if err != nil {
		t.Fatalf("rollout scan: %v", err)
	}
	t.Logf("state db: %d entries, rollout walk: %d entries", len(fromDB), len(fromFiles))

	dbByID := make(map[string]int, len(fromDB))
	for i, e := range fromDB {
		if e.SessionID == "" {
			t.Errorf("state db entry %d has no session id (%s)", i, e.FullPath)
		}
		if e.Provider != provider.Codex {
			t.Errorf("state db entry %s tagged as %s", e.ShortID, e.Provider)
		}
		if _, dup := dbByID[e.SessionID]; dup {
			t.Errorf("duplicate session id %s in state db scan", e.SessionID)
		}
		dbByID[e.SessionID] = i
	}

	// Every thread the rollout walk finds must also be in the state database,
	// otherwise the fast path is silently hiding sessions.
	var missing, cwdMismatch, subagentMismatch int
	for _, f := range fromFiles {
		i, ok := dbByID[f.SessionID]
		if !ok {
			missing++
			if missing <= 3 {
				t.Logf("only in rollout walk: %s (%s)", f.ShortID, f.FullPath)
			}
			continue
		}
		d := fromDB[i]
		if d.ProjectPath != f.ProjectPath {
			cwdMismatch++
			if cwdMismatch <= 3 {
				t.Logf("cwd differs for %s: db=%q file=%q", f.ShortID, d.ProjectPath, f.ProjectPath)
			}
		}
		if d.IsSubagent != f.IsSubagent {
			subagentMismatch++
			if subagentMismatch <= 3 {
				t.Logf("subagent flag differs for %s: db=%v file=%v", f.ShortID, d.IsSubagent, f.IsSubagent)
			}
		}
	}

	if missing > 0 {
		t.Errorf("%d/%d rollouts absent from the state db index", missing, len(fromFiles))
	}
	if cwdMismatch > 0 {
		t.Errorf("%d project paths disagree between the two scans", cwdMismatch)
	}
	if subagentMismatch > 0 {
		t.Errorf("%d subagent classifications disagree between the two scans", subagentMismatch)
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
	real, err := scanCodexRollouts()
	if err != nil || len(real) == 0 {
		t.Skip("no codex rollouts on this machine")
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

	entries, err := ScanCodex()
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
	entries, err := ScanCodex()
	if err != nil {
		t.Skipf("no codex store: %v", err)
	}
	if len(entries) == 0 {
		t.Skip("no codex sessions on this machine")
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
