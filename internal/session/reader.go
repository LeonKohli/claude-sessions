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

// hasCWD reports whether a raw line carries a non-null working directory.
func hasCWD(line []byte) bool {
	return strings.Contains(string(line), `"cwd":"`) ||
		strings.Contains(string(line), `"cwd": "`)
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
		raw := scanner.Bytes()

		// Parse the opening line, every user turn, and — until the working
		// directory is known — any line that carries one. Transcripts often
		// open with last-prompt/mode/permission-mode records whose cwd is null,
		// so reading only line 0 leaves it empty and forces the caller to guess
		// the project from its ambiguously encoded directory name.
		if i > 0 && !containsType(raw, "user") && !(cwd == "" && hasCWD(raw)) {
			continue
		}

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

// ReadClaudeMessages reads up to maxMessages conversation turns from a Claude JSONL file.
func ReadClaudeMessages(path string, maxMessages int) ([]PreviewMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []PreviewMessage
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

		if msg.Type != "user" && msg.Type != "assistant" {
			continue
		}
		if msg.Message == nil {
			continue
		}
		text := strings.TrimSpace(ExtractText(msg.Message.Content))
		if text == "" {
			continue
		}

		msgs = append(msgs, PreviewMessage{
			Role:      msg.Message.Role,
			Text:      text,
			Timestamp: msg.Timestamp,
		})
		if len(msgs) >= maxMessages {
			break
		}
	}

	return msgs, scanner.Err()
}

// ReadClaudeText reads all user/assistant text content from a Claude JSONL file.
// Used for deep search.
func ReadClaudeText(path string) ([]SearchableLine, error) {
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
