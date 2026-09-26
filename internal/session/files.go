package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LeonKohli/claude-sessions/internal/provider"
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
	Recoverable bool `json:"recoverable"`
	Bytes       int  `json:"bytes,omitempty"`
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
// For Claude this is the snapshot taken alongside the edit, read from
// ~/.claude/file-history/<session>/. For Codex it is the verbatim content
// recorded with an add or delete. Codex updates return an error naming the
// diff, because a unified diff cannot reconstruct a file on its own.
func RecoverContent(e SessionEntry, path string) ([]byte, error) {
	if e.Provider == provider.Codex {
		return codexRecover(e.FullPath, path)
	}
	return claudeRecover(e, path)
}

// ─────────────────────────────────────────── Claude

// claudeBackupRef is one tracked-file record inside a file-history-snapshot.
type claudeBackupRef struct {
	BackupFileName *string `json:"backupFileName"`
	Version        int     `json:"version"`
	RealParentDir  string  `json:"realParentDir"`
}

type claudeSnapshotLine struct {
	Type     string `json:"type"`
	Snapshot *struct {
		TrackedFileBackups map[string]claudeBackupRef `json:"trackedFileBackups"`
	} `json:"snapshot"`
}

// claudeFileHistoryDir is where Claude stores snapshot contents for a session.
func claudeFileHistoryDir(sessionID string) string {
	return filepath.Join(provider.ClaudeHome(), "file-history", sessionID)
}

func (r claudeBackupRef) absPath(key string) string {
	if filepath.IsAbs(key) || r.RealParentDir == "" {
		return key
	}
	return filepath.Join(r.RealParentDir, filepath.Base(key))
}

// claudeTracked collects, per file, every backup reference in a session.
func claudeTracked(path string) (map[string][]claudeBackupRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tracked := make(map[string][]claudeBackupRef)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if !strings.Contains(string(raw), "file-history-snapshot") {
			continue
		}
		var line claudeSnapshotLine
		if json.Unmarshal(raw, &line) != nil || line.Snapshot == nil {
			continue
		}
		for key, ref := range line.Snapshot.TrackedFileBackups {
			abs := ref.absPath(key)
			tracked[abs] = append(tracked[abs], ref)
		}
	}
	return tracked, sc.Err()
}

func claudeFileChanges(e SessionEntry) ([]FileChange, error) {
	tracked, err := claudeTracked(e.FullPath)
	if err != nil {
		return nil, err
	}
	histDir := claudeFileHistoryDir(e.SessionID)

	changes := make([]FileChange, 0, len(tracked))
	for path, refs := range tracked {
		sort.Slice(refs, func(i, j int) bool { return refs[i].Version < refs[j].Version })

		// Version 1 with no backup file means there was nothing to preserve:
		// the session created the file.
		kind := ChangeUpdate
		if refs[0].BackupFileName == nil && refs[0].Version <= 1 {
			kind = ChangeAdd
		}

		c := FileChange{Path: path, Kind: kind, Revisions: len(refs)}
		if name := latestBackupName(refs); name != "" {
			if info, err := os.Stat(filepath.Join(histDir, name)); err == nil {
				c.Recoverable = true
				c.Bytes = int(info.Size())
			}
		}
		changes = append(changes, c)
	}
	sortChanges(changes)
	return changes, nil
}

func latestBackupName(refs []claudeBackupRef) string {
	for i := len(refs) - 1; i >= 0; i-- {
		if refs[i].BackupFileName != nil && *refs[i].BackupFileName != "" {
			return *refs[i].BackupFileName
		}
	}
	return ""
}

func claudeRecover(e SessionEntry, want string) ([]byte, error) {
	tracked, err := claudeTracked(e.FullPath)
	if err != nil {
		return nil, err
	}
	refs, ok := tracked[want]
	if !ok {
		if refs, ok = matchBySuffix(tracked, want); !ok {
			return nil, fmt.Errorf("session %s did not track %s", e.ShortID, want)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Version < refs[j].Version })

	name := latestBackupName(refs)
	if name == "" {
		return nil, fmt.Errorf("no snapshot stored for %s (file was created by this session)", want)
	}
	return os.ReadFile(filepath.Join(claudeFileHistoryDir(e.SessionID), name))
}

// ─────────────────────────────────────────── Codex

type codexChange struct {
	Type        string `json:"type"`
	Content     string `json:"content"`
	UnifiedDiff string `json:"unified_diff"`
	MovePath    string `json:"move_path"`
}

type codexPatchLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string                 `json:"type"`
		Changes map[string]codexChange `json:"changes"`
	} `json:"payload"`
}

// codexPatches walks a rollout and returns every recorded patch, in order.
func codexPatches(path string) (map[string][]codexChange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	patches := make(map[string][]codexChange)
	sc := codexScanner(f)
	for sc.Scan() {
		raw := sc.Bytes()
		if !strings.Contains(string(raw), "patch_apply_end") {
			continue
		}
		var line codexPatchLine
		if json.Unmarshal(raw, &line) != nil || line.Payload.Type != "patch_apply_end" {
			continue
		}
		for p, ch := range line.Payload.Changes {
			patches[p] = append(patches[p], ch)
		}
	}
	return patches, sc.Err()
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
		if last.Content != "" && (last.Type == ChangeAdd || last.Type == ChangeDelete) {
			c.Recoverable = true
			c.Bytes = len(last.Content)
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
			return nil, fmt.Errorf("session did not patch %s", want)
		}
	}

	last := list[len(list)-1]
	if last.Content != "" && (last.Type == ChangeAdd || last.Type == ChangeDelete) {
		return []byte(last.Content), nil
	}
	return nil, fmt.Errorf(
		"codex recorded %s as %q, which stores only a unified diff — content is not recoverable byte-exact; use `diff` to read it",
		want, last.Type)
}

// CodexDiff returns the unified diffs recorded for a file, newest last.
func CodexDiff(e SessionEntry, want string) ([]string, error) {
	if e.Provider != provider.Codex {
		return nil, fmt.Errorf("diffs are only recorded by codex sessions")
	}
	patches, err := codexPatches(e.FullPath)
	if err != nil {
		return nil, err
	}
	list, ok := patches[want]
	if !ok {
		if list, ok = matchBySuffix(patches, want); !ok {
			return nil, fmt.Errorf("session did not patch %s", want)
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
