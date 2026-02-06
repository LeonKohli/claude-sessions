package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// EnrichmentData holds the extracted metrics from a full JSONL scan.
type EnrichmentData struct {
	TotalInputTokens  int64
	TotalOutputTokens int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
	Model             string // most-used model
	ToolsUsed         []string
	FilesModified     []string
	MessageCount      int // actual user+assistant count
}

// enrichLine is a lightweight struct for parsing only the fields we need.
type enrichLine struct {
	Type    string `json:"type"`
	Message *struct {
		Role    string `json:"role"`
		Model   string `json:"model"`
		Usage   *Usage `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Snapshot *struct {
		TrackedFileBackups map[string]interface{} `json:"trackedFileBackups"`
	} `json:"snapshot"`
}

// EnrichSession does a full scan of a JSONL file to extract metrics.
func EnrichSession(path string) (*EnrichmentData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data := &EnrichmentData{}
	modelCounts := make(map[string]int)
	toolSet := make(map[string]bool)
	fileSet := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		s := string(raw)

		// Fast path: skip lines that aren't interesting
		isAssistant := strings.Contains(s, `"type": "assistant"`) || strings.Contains(s, `"type":"assistant"`)
		isUser := strings.Contains(s, `"type": "user"`) || strings.Contains(s, `"type":"user"`)
		isSnapshot := strings.Contains(s, `"file-history-snapshot"`)

		if !isAssistant && !isUser && !isSnapshot {
			continue
		}

		var line enrichLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}

		// Count messages
		if (line.Type == "user" || line.Type == "assistant") && line.Message != nil {
			if line.Message.Role == "user" || line.Message.Role == "assistant" {
				data.MessageCount++
			}
		}

		// Extract token usage from assistant messages
		if line.Type == "assistant" && line.Message != nil {
			if line.Message.Usage != nil {
				u := line.Message.Usage
				data.TotalInputTokens += u.InputTokens
				data.TotalOutputTokens += u.OutputTokens
				data.CacheReadTokens += u.CacheReadInputTokens
				data.CacheWriteTokens += u.CacheCreationInputTokens
			}
			if line.Message.Model != "" {
				modelCounts[line.Message.Model]++
			}

			// Extract tool names from content
			extractToolNames(line.Message.Content, toolSet)
		}

		// Extract file names from snapshots
		if line.Type == "file-history-snapshot" && line.Snapshot != nil {
			for fname := range line.Snapshot.TrackedFileBackups {
				fileSet[fname] = true
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
	data.Model = shortenModel(data.Model)

	// Collect tools and files
	for tool := range toolSet {
		data.ToolsUsed = append(data.ToolsUsed, tool)
	}
	for file := range fileSet {
		data.FilesModified = append(data.FilesModified, file)
	}

	return data, scanner.Err()
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

// shortenModel converts full model IDs to readable names.
func shortenModel(model string) string {
	switch {
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
	case model == "":
		return ""
	default:
		// Trim common prefixes
		model = strings.TrimPrefix(model, "claude-")
		if idx := strings.LastIndex(model, "-202"); idx > 0 {
			model = model[:idx]
		}
		return model
	}
}
