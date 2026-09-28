package session

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// ExtractText pulls readable text from a message's Content field.
func ExtractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if m, ok := block.(map[string]interface{}); ok {
				if t, ok := m["type"].(string); ok && t == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ReadFirstUserPrompt reads opening records for prompt text and the earliest recorded CWD.
func ReadFirstUserPrompt(path string) (prompt string, cwd string, gitBranch string, ts time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	maxLines := 100 // look at first 100 lines (some sessions start with many file-history-snapshots)
	for i := 0; i < maxLines && scanner.Scan(); i++ {
		raw := scanner.Bytes()

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		if ts.IsZero() {
			ts = msg.Timestamp
		}
		if cwd == "" && msg.CWD != "" {
			cwd = msg.CWD
		}
		if gitBranch == "" && msg.GitBranch != "" {
			gitBranch = msg.GitBranch
		}

		if msg.Type == "user" && msg.Message != nil && msg.Message.Role == "user" {
			prompt = ExtractText(msg.Message.Content)
			// Keep scanning only if the working directory is still unknown;
			// otherwise everything the caller needs is in hand.
			if cwd != "" {
				return
			}
		}
	}
	return
}

// WalkClaudeText visits user and assistant text until visit returns false.
func WalkClaudeText(ctx context.Context, path string, visit func(SearchableLine) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	lineNum := 0

	for raw, err := range readLines(f) {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		lineNum++
		if !mayContainJSONStrings(raw, "user", "assistant") {
			continue
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		if (msg.Type != "user" && msg.Type != "assistant") || msg.Message == nil {
			continue
		}

		text := ExtractText(msg.Message.Content)
		if strings.TrimSpace(text) == "" {
			continue
		}

		if !visit(SearchableLine{
			Text:      text,
			Role:      msg.Message.Role,
			Timestamp: msg.Timestamp,
			LineNum:   lineNum,
		}) {
			return nil
		}
	}

	return nil
}
