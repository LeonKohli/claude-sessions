package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// enrichLine is a lightweight struct for parsing only the fields we need.
type enrichLine struct {
	Type    string `json:"type"`
	Message *struct {
		ID      string          `json:"id"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Usage   *Usage          `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Snapshot *struct {
		TrackedFileBackups map[string]fileBackup `json:"trackedFileBackups"`
	} `json:"snapshot"`
}

// fileBackup describes one tracked file inside a file-history-snapshot. The map
// key is the path as Claude recorded it, usually relative to the project root,
// while realParentDir carries the absolute directory it actually lived in.
type fileBackup struct {
	BackupFileName string `json:"backupFileName"`
	Version        int    `json:"version"`
	RealParentDir  string `json:"realParentDir"`
}

// absPath resolves a tracked file to an absolute path, so Claude entries are
// comparable with Codex ones, which are always absolute.
func (b fileBackup) absPath(key string) string {
	if filepath.IsAbs(key) || b.RealParentDir == "" {
		return key
	}
	return filepath.Join(b.RealParentDir, filepath.Base(key))
}

// EnrichClaudeSession does a full scan of a JSONL file to extract metrics.
func EnrichClaudeSession(path string) (*EnrichmentData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data := &EnrichmentData{}
	modelCounts := make(map[string]int)
	toolSet := make(map[string]bool)
	fileSet := make(map[string]bool)
	responses := make(map[string]Usage)

	for raw, err := range readLines(f) {
		if err != nil {
			return data, err
		}
		if !mayContainJSONStrings(raw, "assistant", "user", "file-history-snapshot") {
			continue
		}

		var line enrichLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}

		// Count messages
		seenResponse := false
		if line.Type == "assistant" && line.Message != nil && line.Message.ID != "" {
			_, seenResponse = responses[line.Message.ID]
		}
		if (line.Type == "user" || line.Type == "assistant") && line.Message != nil {
			if !seenResponse && (line.Message.Role == "user" || line.Message.Role == "assistant") {
				data.MessageCount++
			}
		}

		// Extract token usage from assistant messages
		if line.Type == "assistant" && line.Message != nil {
			previous := responses[line.Message.ID]
			if line.Message.Usage != nil {
				u := line.Message.Usage
				data.TotalInputTokens += u.InputTokens - previous.InputTokens
				data.TotalOutputTokens += u.OutputTokens - previous.OutputTokens
				data.CacheReadTokens += u.CacheReadInputTokens - previous.CacheReadInputTokens
				data.CacheWriteTokens += u.CacheCreationInputTokens - previous.CacheCreationInputTokens
				previous = *u
			}
			if line.Message.ID != "" {
				responses[line.Message.ID] = previous
			}
			if !seenResponse && line.Message.Model != "" {
				modelCounts[line.Message.Model]++
			}

			// Extract tool names from content
			extractToolNames(line.Message.Content, toolSet)
		}

		// Extract file names from snapshots
		if line.Type == "file-history-snapshot" && line.Snapshot != nil {
			for fname, backup := range line.Snapshot.TrackedFileBackups {
				fileSet[backup.absPath(fname)] = true
			}
		}
	}

	// Find most-used model
	maxCount := 0
	for model, count := range modelCounts {
		if count > maxCount {
			maxCount = count
			data.Model = model
		}
	}

	// Shorten model name for display
	data.Model = ShortenModel(data.Model)

	// Collect tools and files
	for tool := range toolSet {
		data.ToolsUsed = append(data.ToolsUsed, tool)
	}
	for file := range fileSet {
		data.FilesModified = append(data.FilesModified, file)
	}

	return data, nil
}

// extractToolNames finds tool_use blocks in the content array.
func extractToolNames(content json.RawMessage, toolSet map[string]bool) {
	if len(content) == 0 || content[0] != '[' {
		return
	}
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return
	}
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			toolSet[b.Name] = true
		}
	}
}

// ShortenModel converts full model IDs to readable names. Codex model IDs
// (gpt-5.6-sol, codex-auto-review) are already short and pass through.
func ShortenModel(model string) string {
	switch {
	case model == "":
		return ""
	case strings.Contains(model, "opus-4-6"):
		return "opus-4.6"
	case strings.Contains(model, "opus-4-5"):
		return "opus-4.5"
	case strings.Contains(model, "opus-4"):
		return "opus-4"
	case strings.Contains(model, "sonnet-4-5"):
		return "sonnet-4.5"
	case strings.Contains(model, "sonnet-4"):
		return "sonnet-4"
	case strings.Contains(model, "haiku-4-5"):
		return "haiku-4.5"
	case strings.Contains(model, "haiku-4"):
		return "haiku-4"
	case strings.Contains(model, "sonnet-3-5"):
		return "sonnet-3.5"
	default:
		// Trim common prefixes
		model = strings.TrimPrefix(model, "claude-")
		if idx := strings.LastIndex(model, "-202"); idx > 0 {
			model = model[:idx]
		}
		return model
	}
}
