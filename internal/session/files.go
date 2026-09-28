package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

var (
	ErrFileNotTracked  = errors.New("file not tracked")
	ErrNotRecoverable  = errors.New("content not recoverable")
	ErrDiffUnsupported = errors.New("diff unavailable for this provider")
)

// Change kinds. Codex records these exactly; Claude only records that a file
// was tracked, so its kind is inferred from whether a prior version existed.
const (
	ChangeAdd    = "add"
	ChangeUpdate = "update"
	ChangeDelete = "delete"
)

// FileChange summarises what a session did to one file.
type FileChange struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Revisions int    `json:"revisions"`
	// Recoverable reports whether the stored data reproduces file content
	// byte-exactly. Codex updates carry only a unified diff, so they do not.
	Recoverable            bool   `json:"recoverable"`
	Bytes                  int    `json:"bytes,omitempty"`
	RecoverySource         string `json:"recovery_source,omitempty"`
	RecoveryEarlierVersion bool   `json:"recovery_earlier_version,omitempty"`
}

// FileChanges lists every file a session touched.
func FileChanges(e SessionEntry) ([]FileChange, error) {
	if e.Provider == provider.Codex {
		return codexFileChanges(e.FullPath)
	}
	return claudeFileChanges(e)
}

// RecoverContent returns the stored content for one file in a session.
//
// Claude uses checkpoints and successful Write results; Codex uses recorded
// add/delete contents. These are historical versions, not final filesystem state.
func RecoverContent(e SessionEntry, path string) ([]byte, error) {
	if e.Provider == provider.Codex {
		return codexRecover(e.FullPath, path)
	}
	return claudeRecover(e, path)
}

// ─────────────────────────────────────────── Claude

// claudeBackupRef holds checkpoint metadata or bytes from a confirmed Write.
type claudeBackupRef struct {
	BackupFileName *string `json:"backupFileName"`
	Version        int     `json:"version"`
	RealParentDir  string  `json:"realParentDir"`
	content        *string
	kind           string
}

type claudeSnapshotLine struct {
	Type     string `json:"type"`
	Snapshot *struct {
		TrackedFileBackups map[string]claudeBackupRef `json:"trackedFileBackups"`
	} `json:"snapshot"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

func openClaudeHistory(sessionID string) (*os.Root, error) {
	if !filepath.IsLocal(sessionID) {
		return nil, fmt.Errorf("invalid checkpoint session directory %q", sessionID)
	}
	store, err := os.OpenRoot(filepath.Join(provider.ClaudeHome(), "file-history"))
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.OpenRoot(sessionID)
}

func (r claudeBackupRef) absPath(key string) string {
	if filepath.IsAbs(key) || r.RealParentDir == "" {
		return key
	}
	return filepath.Join(r.RealParentDir, filepath.Base(key))
}

// claudeTracked collects distinct checkpoints and confirmed Writes in log order.
func claudeTracked(e SessionEntry) (map[string][]claudeBackupRef, error) {
	f, err := os.Open(e.FullPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tracked := make(map[string][]claudeBackupRef)
	latestWrites := make(map[string]int)
	pendingWrites := make(map[string]bool)
	type backupID struct {
		path, name string
		version    int
	}
	seenBackups := make(map[backupID]bool)
	for raw, err := range readLines(f) {
		if err != nil {
			return tracked, err
		}
		if !mayContainJSONStrings(raw, "file-history-snapshot", "tool_use", "tool_result") {
			continue
		}
		var line claudeSnapshotLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		if line.Type == "file-history-snapshot" && line.Snapshot != nil {
			for key, ref := range line.Snapshot.TrackedFileBackups {
				abs := ref.absPath(key)
				if !filepath.IsAbs(abs) && e.ProjectPath != "" {
					abs = filepath.Join(e.ProjectPath, abs)
				}
				name := ""
				if ref.BackupFileName != nil {
					name = *ref.BackupFileName
				}
				identity := backupID{abs, name, ref.Version}
				if !seenBackups[identity] {
					tracked[abs] = append(tracked[abs], ref)
					seenBackups[identity] = true
				}
			}
		}
		if line.Message == nil {
			continue
		}
		var blocks []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			ToolUseID string `json:"tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if json.Unmarshal(line.Message.Content, &blocks) != nil {
			continue
		}
		resultCount := 0
		for _, block := range blocks {
			if block.Type == "tool_result" {
				resultCount++
			}
		}
		for _, block := range blocks {
			if line.Type == "assistant" && block.Type == "tool_use" && block.Name == "Write" && block.ID != "" {
				pendingWrites[block.ID] = true
			}
			if line.Type != "user" || block.Type != "tool_result" || !pendingWrites[block.ToolUseID] {
				continue
			}
			delete(pendingWrites, block.ToolUseID)
			if resultCount != 1 {
				continue
			}
			var result struct {
				Type     string  `json:"type"`
				FilePath string  `json:"filePath"`
				Content  *string `json:"content"`
			}
			if block.IsError || json.Unmarshal(line.ToolUseResult, &result) != nil || result.FilePath == "" || result.Content == nil {
				continue
			}
			kind := ChangeUpdate
			if result.Type == "create" {
				kind = ChangeAdd
			} else if result.Type != "update" {
				continue
			}
			refs := tracked[result.FilePath]
			if previous, ok := latestWrites[result.FilePath]; ok {
				refs[previous].content = nil
			}
			latestWrites[result.FilePath] = len(refs)
			tracked[result.FilePath] = append(refs, claudeBackupRef{content: result.Content, kind: kind})
		}
	}
	return tracked, nil
}

func claudeFileChanges(e SessionEntry) ([]FileChange, error) {
	tracked, err := claudeTracked(e)
	if err != nil {
		return nil, err
	}
	// Confirmed Writes remain recoverable without a checkpoint directory.
	history, _ := openClaudeHistory(e.SessionID)
	if history != nil {
		defer history.Close()
	}

	changes := make([]FileChange, 0, len(tracked))
	for path, refs := range tracked {
		// Version 1 with no backup file means there was nothing to preserve:
		// the session created the file.
		kind := ChangeUpdate
		if refs[0].kind != "" {
			kind = refs[0].kind
		} else if refs[0].BackupFileName == nil && refs[0].Version <= 1 {
			kind = ChangeAdd
		}

		c := FileChange{Path: path, Kind: kind, Revisions: len(refs)}
		ref, size, earlier := latestClaudeContent(refs, history)
		if ref.content != nil {
			c.Recoverable, c.Bytes, c.RecoverySource = true, size, "claude_write"
		} else if ref.BackupFileName != nil {
			c.Recoverable, c.Bytes, c.RecoverySource = true, size, "claude_checkpoint"
		}
		c.RecoveryEarlierVersion = c.Recoverable && earlier
		changes = append(changes, c)
	}
	sortChanges(changes)
	return changes, nil
}

func latestClaudeContent(refs []claudeBackupRef, history *os.Root) (claudeBackupRef, int, bool) {
	earlier := false
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		if ref.content != nil {
			return ref, len(*ref.content), earlier
		}
		if ref.BackupFileName != nil && *ref.BackupFileName != "" {
			if history != nil {
				if info, err := history.Stat(*ref.BackupFileName); err == nil && info.Mode().IsRegular() {
					return ref, int(info.Size()), earlier
				}
			}
			earlier = true
		}
	}
	return claudeBackupRef{}, 0, earlier
}

func claudeRecover(e SessionEntry, want string) ([]byte, error) {
	tracked, err := claudeTracked(e)
	if err != nil {
		return nil, err
	}
	refs, ok := tracked[want]
	if !ok {
		if refs, ok = matchBySuffix(tracked, want); !ok {
			return nil, fmt.Errorf("%w: session %s did not track %s", ErrFileNotTracked, e.ShortID, want)
		}
	}
	history, _ := openClaudeHistory(e.SessionID)
	if history != nil {
		defer history.Close()
	}
	ref, _, _ := latestClaudeContent(refs, history)
	if ref.content != nil {
		return []byte(*ref.content), nil
	}
	if ref.BackupFileName == nil {
		return nil, fmt.Errorf("%w for %s: no readable checkpoint or confirmed Write result", ErrNotRecoverable, want)
	}
	return history.ReadFile(*ref.BackupFileName)
}

// ─────────────────────────────────────────── Codex

type codexChange struct {
	Type        string  `json:"type"`
	Content     *string `json:"content"`
	UnifiedDiff string  `json:"unified_diff"`
	MovePath    string  `json:"move_path"`
}

func codexPatchChanges(payload json.RawMessage) (string, map[string]codexChange) {
	var event struct {
		Type    string                 `json:"type"`
		CallID  string                 `json:"call_id"`
		Success bool                   `json:"success"`
		Changes map[string]codexChange `json:"changes"`
		Item    struct {
			Type    string                 `json:"type"`
			ID      string                 `json:"id"`
			Status  string                 `json:"status"`
			Changes map[string]codexChange `json:"changes"`
		} `json:"item"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return "", nil
	}
	if event.Type == "patch_apply_end" && event.Success {
		return event.CallID, event.Changes
	}
	if event.Type == "item_completed" && event.Item.Type == "FileChange" && event.Item.Status == "completed" {
		return event.Item.ID, event.Item.Changes
	}
	return "", nil
}

// codexPatches walks a rollout and returns every recorded patch, in order.
func codexPatches(path string) (map[string][]codexChange, error) {
	patches := make(map[string][]codexChange)
	seen := make(map[string]bool)
	for source, err := range transcriptLines(provider.Codex, path) {
		if err != nil {
			return patches, err
		}
		raw := source.raw
		if !mayContainJSONStrings(raw, "patch_apply_end", "FileChange") {
			continue
		}
		var line codexLine
		if json.Unmarshal(raw, &line) != nil || line.Type != "event_msg" {
			continue
		}
		id, changes := codexPatchChanges(line.Payload)
		if changes == nil || (id != "" && seen[id]) {
			continue
		}
		if id != "" {
			seen[id] = true
		}
		for p, ch := range changes {
			patches[p] = append(patches[p], ch)
		}
	}
	return patches, nil
}

func codexFileChanges(path string) ([]FileChange, error) {
	patches, err := codexPatches(path)
	if err != nil {
		return nil, err
	}

	changes := make([]FileChange, 0, len(patches))
	for p, list := range patches {
		last := list[len(list)-1]
		c := FileChange{Path: p, Kind: last.Type, Revisions: len(list)}
		// Adds and deletes carry the file verbatim; updates carry only a diff.
		if last.Content != nil && (last.Type == ChangeAdd || last.Type == ChangeDelete) {
			c.Recoverable = true
			c.Bytes = len(*last.Content)
		}
		changes = append(changes, c)
	}
	sortChanges(changes)
	return changes, nil
}

func codexRecover(rolloutPath, want string) ([]byte, error) {
	patches, err := codexPatches(rolloutPath)
	if err != nil {
		return nil, err
	}
	list, ok := patches[want]
	if !ok {
		if list, ok = matchBySuffix(patches, want); !ok {
			return nil, fmt.Errorf("%w: session did not patch %s", ErrFileNotTracked, want)
		}
	}

	last := list[len(list)-1]
	if last.Content != nil && (last.Type == ChangeAdd || last.Type == ChangeDelete) {
		return []byte(*last.Content), nil
	}
	return nil, fmt.Errorf(
		"%w: codex recorded %s as %q, which stores only a unified diff; use `diff` to read it",
		ErrNotRecoverable, want, last.Type)
}

// CodexDiff returns the unified diffs recorded for a file, newest last.
func CodexDiff(e SessionEntry, want string) ([]string, error) {
	if e.Provider != provider.Codex {
		return nil, fmt.Errorf("%w: diffs are only recorded by codex sessions", ErrDiffUnsupported)
	}
	patches, err := codexPatches(e.FullPath)
	if err != nil {
		return nil, err
	}
	list, ok := patches[want]
	if !ok {
		if list, ok = matchBySuffix(patches, want); !ok {
			return nil, fmt.Errorf("%w: session did not patch %s", ErrFileNotTracked, want)
		}
	}
	var out []string
	for _, c := range list {
		if c.UnifiedDiff != "" {
			out = append(out, c.UnifiedDiff)
		}
	}
	return out, nil
}

// ─────────────────────────────────────────── shared

// matchBySuffix lets a caller pass a relative path or bare filename when the
// stored key is absolute, which is what an agent typically has to hand.
func matchBySuffix[T any](m map[string][]T, want string) ([]T, bool) {
	want = strings.TrimPrefix(want, "./")
	var hit []T
	found := 0
	for k, v := range m {
		if k == want || strings.HasSuffix(k, "/"+want) {
			hit, found = v, found+1
		}
	}
	return hit, found == 1 // ambiguous matches are not a match
}

func sortChanges(c []FileChange) {
	sort.Slice(c, func(i, j int) bool { return c[i].Path < c[j].Path })
}
