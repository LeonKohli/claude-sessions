package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leon/claude-sessions/internal/session"
	"github.com/leon/claude-sessions/internal/util"
)

// ScanAll walks ~/.claude/projects/ and builds a complete session index.
func ScanAll() ([]session.SessionEntry, error) {
	projectsDir := util.ClaudeProjectsDir()
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	var allSessions []session.SessionEntry
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

		// Scan for JSONL files not covered by the index (recursive walk)
		filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			if indexed[path] {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasPrefix(base, "agent-") {
				return nil
			}

			s, err := scanJSONLFile(path, originalPath, dirName)
			if err != nil {
				return nil
			}
			// Skip empty sessions (no user messages, no summary)
			if s.FirstPrompt == "" && s.Summary == "" {
				return nil
			}
			allSessions = append(allSessions, s)
			return nil
		})
	}

	return allSessions, nil
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
		if e.IsSidechain {
			continue
		}

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

		shortID := e.SessionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		results = append(results, session.SessionEntry{
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
			FileMtime:    int64(e.FileMtime),
			FileSize:     info.Size(),
			ShortID:      shortID,
		})
	}

	return results, idx.OriginalPath, nil
}

// scanJSONLFile extracts session metadata from a raw JSONL file.
func scanJSONLFile(path, originalPath, dirName string) (session.SessionEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return session.SessionEntry{}, err
	}

	prompt, cwd, gitBranch, ts := session.ReadFirstUserPrompt(path)

	sessionID := filepath.Base(path)
	sessionID = strings.TrimSuffix(sessionID, ".jsonl")

	projectPath := util.ResolveProjectPath(originalPath, "", cwd, dirName)

	shortID := sessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	created := ts
	if created.IsZero() {
		created = info.ModTime()
	}

	return session.SessionEntry{
		SessionID:   sessionID,
		FullPath:    path,
		FirstPrompt: util.Truncate(util.CleanPrompt(prompt), 200),
		Created:     created,
		Modified:    info.ModTime(),
		GitBranch:   gitBranch,
		ProjectPath: projectPath,
		FileMtime:   info.ModTime().UnixMilli(),
		FileSize:    info.Size(),
		ShortID:     shortID,
	}, nil
}
