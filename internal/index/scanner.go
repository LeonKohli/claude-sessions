package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/LeonKohli/claude-sessions/internal/util"
)

// ScanClaude walks ~/.claude/projects/ and builds the Claude half of the index.
func ScanClaude() ([]session.SessionEntry, error) {
	projectsDir := provider.ClaudeProjectsDir()
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	var allSessions []session.SessionEntry
	var uncovered []claudeScanJob
	// Track which JSONL files are already covered by sessions-index.json
	indexed := make(map[string]bool)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		dirPath := filepath.Join(projectsDir, dirName)

		// Try to read sessions-index.json
		indexPath := filepath.Join(dirPath, "sessions-index.json")
		originalPath := ""
		if idxSessions, origPath, err := parseSessionsIndex(indexPath, dirName); err == nil {
			originalPath = origPath
			for _, s := range idxSessions {
				allSessions = append(allSessions, s)
				indexed[s.FullPath] = true
			}
		}

		// Queue JSONL files the index does not cover for a parallel pass.
		filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") || indexed[path] {
				return nil
			}
			uncovered = append(uncovered, claudeScanJob{path: path, originalPath: originalPath, dirName: dirName})
			return nil
		})
	}

	return append(allSessions, scanClaudeFiles(uncovered)...), nil
}

type claudeScanJob struct {
	path         string
	originalPath string
	dirName      string
}

// scanClaudeFiles reads session headers concurrently. Subagent transcripts make
// up well over half the store, so a serial pass here dominates the cold scan.
func scanClaudeFiles(jobs []claudeScanJob) []session.SessionEntry {
	if len(jobs) == 0 {
		return nil
	}

	queue := make(chan claudeScanJob, len(jobs))
	for _, j := range jobs {
		queue <- j
	}
	close(queue)

	results := make(chan session.SessionEntry, len(jobs))
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				s, err := scanJSONLFile(j.path, j.originalPath, j.dirName)
				if err != nil {
					continue
				}
				// Skip empty sessions (no user messages, no summary)
				if s.FirstPrompt == "" && s.Summary == "" {
					continue
				}
				results <- s
			}
		}()
	}
	wg.Wait()
	close(results)

	out := make([]session.SessionEntry, 0, len(jobs))
	for s := range results {
		out = append(out, s)
	}
	return out
}

// parseSessionsIndex reads a sessions-index.json and returns entries + originalPath.
func parseSessionsIndex(path, dirName string) ([]session.SessionEntry, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	var idx session.SessionsIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, "", err
	}

	var results []session.SessionEntry
	for _, e := range idx.Entries {
		// Skip ghost entries — file must actually exist on disk
		info, err := os.Stat(e.FullPath)
		if err != nil {
			continue
		}

		// Skip empty sessions — no conversation content at all
		if e.MessageCount == 0 && e.Summary == "" && e.FirstPrompt == "" {
			continue
		}

		created, _ := time.Parse(time.RFC3339Nano, e.Created)
		modified, _ := time.Parse(time.RFC3339Nano, e.Modified)

		projectPath := util.ResolveProjectPath(idx.OriginalPath, e.ProjectPath, "", dirName)

		results = append(results, session.SessionEntry{
			Provider:     provider.Claude,
			SessionID:    e.SessionID,
			FullPath:     e.FullPath,
			Summary:      e.Summary,
			FirstPrompt:  e.FirstPrompt,
			MessageCount: e.MessageCount,
			Created:      created,
			Modified:     modified,
			GitBranch:    e.GitBranch,
			ProjectPath:  projectPath,
			IsSidechain:  e.IsSidechain,
			IsSubagent:   e.IsSidechain || isClaudeAgentFile(e.FullPath),
			FileMtime:    int64(e.FileMtime),
			FileSize:     info.Size(),
			ShortID:      shortID(e.SessionID),
		})
	}

	return results, idx.OriginalPath, nil
}

// isClaudeAgentFile reports whether a transcript belongs to a spawned subagent
// rather than a resumable top-level session.
func isClaudeAgentFile(path string) bool {
	return strings.HasPrefix(filepath.Base(path), "agent-")
}

// claudeSessionID derives a unique id from a transcript path.
//
// Subagent transcripts live at <parent>/subagents/agent-<id>.jsonl, and Claude
// reuses the same agent id under different parents, so the bare filename is not
// unique. Qualifying it with the parent session both disambiguates and records
// which session spawned it.
func claudeSessionID(path string) (id, parent string) {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if !isClaudeAgentFile(path) {
		return base, ""
	}
	dir := filepath.Dir(path)
	if filepath.Base(dir) != "subagents" {
		return base, ""
	}
	parent = filepath.Base(filepath.Dir(dir))
	return parent + "/" + base, parent
}

// scanJSONLFile extracts session metadata from a raw JSONL file.
func scanJSONLFile(path, originalPath, dirName string) (session.SessionEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return session.SessionEntry{}, err
	}

	prompt, cwd, gitBranch, ts := session.ReadFirstUserPrompt(path)

	sessionID, parent := claudeSessionID(path)
	projectPath := util.ResolveProjectPath(originalPath, "", cwd, dirName)

	created := ts
	if created.IsZero() {
		created = info.ModTime()
	}

	return session.SessionEntry{
		Provider:    provider.Claude,
		SessionID:   sessionID,
		FullPath:    path,
		FirstPrompt: util.Truncate(util.CleanPrompt(prompt), 200),
		Created:     created,
		Modified:    info.ModTime(),
		GitBranch:   gitBranch,
		ProjectPath: projectPath,
		IsSubagent:  isClaudeAgentFile(path),
		Parent:      parent,
		FileMtime:   info.ModTime().UnixMilli(),
		FileSize:    info.Size(),
		ShortID:     shortID(sessionID),
	}, nil
}
