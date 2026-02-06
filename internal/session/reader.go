package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// containsType checks if a JSONL line contains "type":"val" or "type": "val".
func containsType(line []byte, val string) bool {
	s := string(line)
	return strings.Contains(s, `"type":"`+val+`"`) ||
		strings.Contains(s, `"type": "`+val+`"`)
}

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

// ReadFirstMessage reads and parses only the first line of a JSONL file.
func ReadFirstMessage(path string) (*Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("empty file: %s", path)
	}

	var msg Message
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ReadFirstUserPrompt reads the first few lines to find the first user message text.
// Returns the text and CWD from the first line (which always has CWD even if not a user msg).
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
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}

		// Capture CWD and branch from first line
		if i == 0 {
			cwd = msg.CWD
			gitBranch = msg.GitBranch
			ts = msg.Timestamp
		}

		if msg.Type == "user" && msg.Message != nil && msg.Message.Role == "user" {
			prompt = ExtractText(msg.Message.Content)
			if ts.IsZero() {
				ts = msg.Timestamp
			}
			return
		}
	}
	return
}

// ReadMessages reads up to maxMessages user/assistant messages from a JSONL file.
func ReadMessages(path string, maxMessages int) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024) // 2MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()

		// Quick pre-filter: only parse user/assistant messages
		if !containsType(line, "user") && !containsType(line, "assistant") {
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Type == "user" || msg.Type == "assistant" {
			msgs = append(msgs, msg)
			if len(msgs) >= maxMessages {
				break
			}
		}
	}

	return msgs, scanner.Err()
}

// ReadAllText reads all user/assistant text content from a JSONL file.
// Used for deep search.
func ReadAllText(path string) ([]SearchableLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []SearchableLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		raw := scanner.Bytes()

		// Quick pre-filter
		if !containsType(raw, "user") && !containsType(raw, "assistant") {
			continue
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		if msg.Message == nil {
			continue
		}

		text := ExtractText(msg.Message.Content)
		if text == "" {
			continue
		}

		lines = append(lines, SearchableLine{
			Text:      text,
			Role:      msg.Message.Role,
			Timestamp: msg.Timestamp,
			LineNum:   lineNum,
		})
	}

	return lines, scanner.Err()
}

// SearchableLine is a text line extracted from a session for deep search.
type SearchableLine struct {
	Text      string
	Role      string
	Timestamp time.Time
	LineNum   int
}
