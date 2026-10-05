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
		role, text, ts, ok := claudeLineText(raw)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}

		if !visit(SearchableLine{
			Text:      text,
			Role:      role,
			Timestamp: ts,
			LineNum:   lineNum,
		}) {
			return nil
		}
	}

	return nil
}

// claudeLineText decodes only the fields a searchable message needs, so tool
// results beside them are skipped rather than decoded.
func claudeLineText(raw []byte) (role, text string, ts time.Time, ok bool) {
	var kind, message, stamp []byte
	if !eachMember(raw, func(key string, value []byte) bool {
		switch key {
		case "type":
			kind = value
		case "message":
			message = value
		case "timestamp":
			stamp = value
		}
		return true
	}) {
		return "", "", time.Time{}, false
	}
	var typ string
	if json.Unmarshal(kind, &typ) != nil || (typ != "user" && typ != "assistant") || message == nil || string(message) == "null" {
		return "", "", time.Time{}, false
	}
	if stamp != nil && json.Unmarshal(stamp, &ts) != nil {
		return "", "", time.Time{}, false
	}
	var roleValue, content []byte
	if !eachMember(message, func(key string, value []byte) bool {
		switch key {
		case "role":
			roleValue = value
		case "content":
			content = value
		}
		return true
	}) {
		return "", "", time.Time{}, false
	}
	if roleValue != nil && json.Unmarshal(roleValue, &role) != nil {
		return "", "", time.Time{}, false
	}
	text, ok = contentText(content)
	return role, text, ts, ok
}

// contentText mirrors ExtractText for undecoded message content.
func contentText(content []byte) (string, bool) {
	if len(content) == 0 {
		return "", true
	}
	switch content[0] {
	case '"':
		var text string
		err := json.Unmarshal(content, &text)
		return text, err == nil
	case '[':
		var parts []string
		ok := eachElement(content, func(block []byte) bool {
			var kind, text []byte
			if len(block) == 0 || block[0] != '{' {
				return true
			}
			eachMember(block, func(key string, value []byte) bool {
				switch key {
				case "type":
					kind = value
				case "text":
					text = value
				}
				return true
			})
			var typ, part string
			if json.Unmarshal(kind, &typ) == nil && typ == "text" && json.Unmarshal(text, &part) == nil {
				parts = append(parts, part)
			}
			return true
		})
		return strings.Join(parts, "\n"), ok
	}
	return "", true
}
