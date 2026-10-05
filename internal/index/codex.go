package index

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/LeonKohli/claude-sessions/internal/util"
)

// scanCodex builds the Codex half of the index. Codex maintains its own
// SQLite thread index — the same one `codex resume` trusts — which carries the
// title, token total and model without touching a single rollout file. When it
// is missing or its schema has drifted, fall back to reading the rollouts.
func scanCodex(cache *metadataCache) ([]session.SessionEntry, error) {
	if entries, err := scanCodexStateDB(cache); err == nil && len(entries) > 0 {
		return entries, nil
	} else if errors.Is(err, ErrStoreUnavailable) {
		return nil, err
	}
	return scanCodexRollouts(cache)
}

func scanCodexStateDB(cache *metadataCache) ([]session.SessionEntry, error) {
	dbPath := provider.CodexStateDB()
	if dbPath == "" {
		return nil, fmt.Errorf("no codex state database")
	}

	// Read-only with a short busy timeout: Codex may be mid-write, and we must
	// never mutate its live index.
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&_pragma=busy_timeout(3000)"}).String())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM threads")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	for _, required := range []string{"id", "rollout_path", "cwd", "created_at", "updated_at"} {
		if !slices.Contains(cols, required) {
			return nil, fmt.Errorf("threads table missing %q", required)
		}
	}

	var entries []session.SessionEntry
	for rows.Next() {
		vals := make([]sql.RawBytes, len(cols))
		dest := make([]any, len(cols))
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		field := make(map[string]string, len(cols))
		for i, c := range cols {
			field[c] = string(vals[i])
		}

		path := field["rollout_path"]
		// Rows can outlive their file after manual cleanup or a migration.
		info, err := os.Stat(path)
		if err != nil {
			if field["history_mode"] == "paginated" {
				return nil, fmt.Errorf("%w: selected rollout for %s: %w", ErrStoreUnavailable, field["id"], err)
			}
			continue
		}

		meta := session.CodexMeta{
			ThreadSource: field["thread_source"],
			Source:       json.RawMessage(field["source"]),
			Nickname:     field["agent_nickname"],
			Role:         field["agent_role"],
		}
		e := session.SessionEntry{
			Provider:    provider.Codex,
			SessionID:   field["id"],
			FullPath:    path,
			ProjectPath: field["cwd"],
			GitBranch:   field["git_branch"],
			Summary:     util.Truncate(util.CleanPrompt(field["title"]), 200),
			FirstPrompt: util.Truncate(util.CleanPrompt(field["first_user_message"]), 200),
			Model:       session.ShortenModel(field["model"]),
			Created:     codexTime(field["created_at_ms"], field["created_at"]),
			Modified:    codexTime(field["updated_at_ms"], field["updated_at"]),
			Archived:    field["archived"] == "1",
			IsSubagent:  meta.IsSubagent(),
			AgentLabel:  meta.AgentLabel(),
			Parent:      codexParentFromSource(field["source"]),
			FileMtime:   info.ModTime().UnixMilli(),
			FileSize:    info.Size(),
			ShortID:     shortID(field["id"]),
		}
		if tokens := parseInt(field["tokens_used"]); tokens > 0 {
			// tokens_used is the running total Codex reports, which already
			// folds input and output together. Attribute it to input so the
			// token sort has a value; enrichment splits it properly later.
			e.TotalInputTokens = tokens
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return backfillUntitled(cache, entries)
}

// backfillUntitled recovers display text for threads the state database never
// titled. Roughly a sixth of rows carry an empty title and first_user_message
// while their transcript holds a real prompt, so trusting the database alone
// would hide them. Entries with no prompt in either place have no conversation
// and are dropped.
func backfillUntitled(cache *metadataCache, entries []session.SessionEntry) ([]session.SessionEntry, error) {
	reader := new(session.CodexReader)
	readErrors := make([]error, len(entries))
	var pending []int
	for i, e := range entries {
		if e.Summary == "" && e.FirstPrompt == "" {
			pending = append(pending, i)
		}
	}

	if len(pending) > 0 {
		jobs := make(chan int, len(pending))
		for _, i := range pending {
			jobs <- i
		}
		close(jobs)

		var wg sync.WaitGroup
		for w := 0; w < min(2, runtime.GOMAXPROCS(0), len(pending)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					_, prompt, model, _, ok, err := readCodexHead(cache, reader, entries[i].FullPath)
					if err != nil {
						readErrors[i] = err
						continue
					}
					if !ok {
						continue
					}
					entries[i].FirstPrompt = util.Truncate(util.CleanPrompt(prompt), 200)
					if entries[i].Model == "" {
						entries[i].Model = session.ShortenModel(model)
					}
				}
			}()
		}
		wg.Wait()
	}
	for i, err := range readErrors {
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrStoreUnavailable, entries[i].FullPath, err)
		}
	}

	kept := entries[:0]
	for _, e := range entries {
		if e.Summary == "" && e.FirstPrompt == "" {
			continue // no conversation content anywhere
		}
		kept = append(kept, e)
	}
	return kept, nil
}

// scanCodexRollouts reads session_meta headers straight from the rollout files.
// Used when the state database is absent or unreadable.
func scanCodexRollouts(cache *metadataCache) ([]session.SessionEntry, error) {
	reader := new(session.CodexReader)
	var paths []string
	for _, root := range []string{provider.CodexSessionsDir(), provider.CodexArchivedDir()} {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				if p == root && os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if p == root && !d.IsDir() {
				return fmt.Errorf("%s is not a directory", root)
			}
			if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
				return nil
			}
			paths = append(paths, p)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	jobs := make(chan string, len(paths))
	for _, p := range paths {
		jobs <- p
	}
	close(jobs)

	archivedDir := provider.CodexArchivedDir()
	results := make(chan session.SessionEntry, len(paths))
	readErrors := make(chan error, len(paths))
	var wg sync.WaitGroup
	for i := 0; i < min(2, runtime.GOMAXPROCS(0), len(paths)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				meta, prompt, model, ts, ok, err := readCodexHead(cache, reader, path)
				if err != nil {
					readErrors <- fmt.Errorf("%w: %s: %w", ErrStoreUnavailable, path, err)
					continue
				}
				if !ok {
					continue
				}
				id := meta.ID
				if id == "" {
					id = codexIDFromFilename(path)
				}
				if prompt == "" {
					continue // no user turn: nothing to show or resume into
				}
				results <- session.SessionEntry{
					Provider:    provider.Codex,
					SessionID:   id,
					FullPath:    path,
					ProjectPath: meta.CWD,
					GitBranch:   gitBranch(meta),
					FirstPrompt: util.Truncate(util.CleanPrompt(prompt), 200),
					Model:       session.ShortenModel(model),
					Created:     ts,
					Modified:    info.ModTime(),
					Archived:    strings.HasPrefix(path, archivedDir),
					IsSubagent:  meta.IsSubagent(),
					AgentLabel:  meta.AgentLabel(),
					Parent:      meta.Parent(),
					FileMtime:   info.ModTime().UnixMilli(),
					FileSize:    info.Size(),
					ShortID:     shortID(id),
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	close(readErrors)
	for err := range readErrors {
		return nil, err
	}

	var entries []session.SessionEntry
	for e := range results {
		entries = append(entries, e)
	}
	return entries, nil
}

func gitBranch(m session.CodexMeta) string {
	if m.Git == nil {
		return ""
	}
	return m.Git.Branch
}

// codexIDFromFilename recovers the thread UUID from rollout-<ISO>-<uuid>.jsonl.
func codexIDFromFilename(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	base = strings.TrimPrefix(base, "rollout-")
	// The ISO timestamp prefix is a fixed 19 characters (2026-07-27T17-15-47).
	if len(base) > 20 {
		return base[20:]
	}
	return base
}

// shortIDLen is deliberately longer than the conventional 8. Codex ids are
// UUIDv7, so sessions created in the same span share a long time prefix, and
// Claude's subagent ids all begin "agent-". Measured across this store, an
// 8-character prefix is ambiguous for 64% of Claude sessions and 37% of Codex
// ones; 12 characters resolves every session but the true duplicates.
const shortIDLen = 12

func shortID(id string) string {
	// A qualified subagent id is "<parent>/agent-xxxx"; the agent half is what
	// distinguishes it from its siblings, so that is the useful short form.
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > shortIDLen {
		return id[:shortIDLen]
	}
	return id
}

func parseInt(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	if s == "" {
		return 0
	}
	return n
}

// codexTime prefers the millisecond column and falls back to whole seconds.
func codexTime(ms, secs string) time.Time {
	if v := parseInt(ms); v > 0 {
		return time.UnixMilli(v)
	}
	if v := parseInt(secs); v > 0 {
		return time.Unix(v, 0)
	}
	return time.Time{}
}

// codexParentFromSource pulls the spawning thread out of the threads.source
// column, which stores the same object the rollout header carries.
func codexParentFromSource(src string) string {
	if !strings.Contains(src, "thread_spawn") {
		return ""
	}
	var s struct {
		Subagent struct {
			ThreadSpawn struct {
				ParentThreadID string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	}
	if json.Unmarshal([]byte(src), &s) != nil {
		return ""
	}
	return s.Subagent.ThreadSpawn.ParentThreadID
}
