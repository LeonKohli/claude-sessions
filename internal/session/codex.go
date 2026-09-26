package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Codex writes one JSONL "rollout" file per thread. Every line is an envelope
// {timestamp, type, payload}; the first is always session_meta. See
// codex-rs/state/src/sqlite.rs and codex-rs/tui/src/session_resume.rs upstream.
type codexLine struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// CodexMeta is the payload of the leading session_meta line.
type CodexMeta struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	CWD          string          `json:"cwd"`
	Originator   string          `json:"originator"`
	CliVersion   string          `json:"cli_version"`
	ThreadSource string          `json:"thread_source"`
	Source       json.RawMessage `json:"source"`
	Nickname     string          `json:"agent_nickname"`
	Role         string          `json:"agent_role"`
	Git          *struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

// IsSubagent reports whether this thread was spawned by another thread rather
// than started by the user. Older rollouts predate thread_source and only
// record it inside the free-form source object.
func (m CodexMeta) IsSubagent() bool {
	return m.ThreadSource == "subagent" || strings.Contains(string(m.Source), "subagent")
}

// Parent returns the thread that spawned this one, or "" for a top-level
// session. Codex records it twice: session_id on the envelope points at the
// root thread, and source.subagent.thread_spawn carries the direct parent.
func (m CodexMeta) Parent() string {
	if !m.IsSubagent() {
		return ""
	}
	var src struct {
		Subagent struct {
			ThreadSpawn struct {
				ParentThreadID string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	}
	if json.Unmarshal(m.Source, &src) == nil && src.Subagent.ThreadSpawn.ParentThreadID != "" {
		return src.Subagent.ThreadSpawn.ParentThreadID
	}
	if m.SessionID != "" && m.SessionID != m.ID {
		return m.SessionID
	}
	return ""
}

// AgentLabel renders a spawned thread's identity, e.g. "Wegener/explorer".
func (m CodexMeta) AgentLabel() string {
	switch {
	case m.Nickname != "" && m.Role != "":
		return m.Nickname + "/" + m.Role
	case m.Nickname != "":
		return m.Nickname
	default:
		return m.Role
	}
}

type codexPayloadType struct {
	Type string `json:"type"`
}

type codexTurnContext struct {
	CWD    string `json:"cwd"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type codexTextEvent struct {
	Message string `json:"message"`
}

type codexTokenCount struct {
	Info *struct {
		Total *struct {
			InputTokens       int64 `json:"input_tokens"`
			CachedInputTokens int64 `json:"cached_input_tokens"`
			CacheWriteTokens  int64 `json:"cache_write_input_tokens"`
			OutputTokens      int64 `json:"output_tokens"`
		} `json:"total_token_usage"`
	} `json:"info"`
}

type codexFunctionCall struct {
	Name string `json:"name"`
}

type codexPatchApply struct {
	Changes map[string]json.RawMessage `json:"changes"`
}

func codexScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	// Rollout lines embed full tool output and base instructions; 8MB covers
	// the largest observed lines with room to spare.
	sc.Buffer(make([]byte, 8*1024*1024), 8*1024*1024)
	return sc
}

// headScanBudget bounds how much of a transcript the header scan will read.
// The first user_message normally lands within ten lines, but resumed threads
// replay their prior history first and can push it past line 10,000, so the
// stop condition is a byte budget rather than a line count.
const headScanBudget = 16 << 20

// ReadCodexHead parses the session_meta line plus the first user message and
// the initial turn model, reading only as far into the file as it must.
func ReadCodexHead(path string) (meta CodexMeta, prompt, model string, ts time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := codexScanner(f)
	read := 0
	for i := 0; sc.Scan(); i++ {
		raw := sc.Bytes()
		read += len(raw)
		if read > headScanBudget {
			return
		}

		// Only the opening line and two event kinds matter here; skipping the
		// JSON parse for everything else keeps a deep scan cheap.
		if i > 0 && !containsType(raw, "turn_context") && !bytesContain(raw, `"user_message"`) {
			continue
		}

		var line codexLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}

		switch line.Type {
		case "session_meta":
			if json.Unmarshal(line.Payload, &meta) != nil {
				return CodexMeta{}, "", "", time.Time{}, false
			}
			ts = line.Timestamp
			ok = true

		case "turn_context":
			var tc codexTurnContext
			if json.Unmarshal(line.Payload, &tc) == nil && model == "" {
				model = tc.Model
			}

		case "event_msg":
			var pt codexPayloadType
			if json.Unmarshal(line.Payload, &pt) != nil || pt.Type != "user_message" {
				continue
			}
			var ev codexTextEvent
			if json.Unmarshal(line.Payload, &ev) == nil {
				prompt = ev.Message
				return
			}
		}
	}
	return
}

func bytesContain(b []byte, sub string) bool {
	return strings.Contains(string(b), sub)
}

// codexEventText maps an event_msg payload to a conversation turn.
// Returns an empty role for events that are not part of the dialogue.
func codexEventText(payload json.RawMessage) (role, text string) {
	var pt codexPayloadType
	if json.Unmarshal(payload, &pt) != nil {
		return "", ""
	}
	switch pt.Type {
	case "user_message":
		role = "user"
	case "agent_message":
		role = "assistant"
	default:
		return "", ""
	}
	var ev codexTextEvent
	if json.Unmarshal(payload, &ev) != nil {
		return "", ""
	}
	return role, strings.TrimSpace(ev.Message)
}

// ReadCodexMessages reads up to maxMessages conversation turns from a rollout.
func ReadCodexMessages(path string, maxMessages int) ([]PreviewMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []PreviewMessage
	sc := codexScanner(f)
	for sc.Scan() {
		raw := sc.Bytes()
		if !containsType(raw, "event_msg") {
			continue
		}
		var line codexLine
		if json.Unmarshal(raw, &line) != nil || line.Type != "event_msg" {
			continue
		}
		role, text := codexEventText(line.Payload)
		if role == "" || text == "" {
			continue
		}
		msgs = append(msgs, PreviewMessage{Role: role, Text: text, Timestamp: line.Timestamp})
		if len(msgs) >= maxMessages {
			break
		}
	}
	return msgs, sc.Err()
}

// ReadCodexText extracts every conversation line from a rollout for deep search.
func ReadCodexText(path string) ([]SearchableLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []SearchableLine
	sc := codexScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		raw := sc.Bytes()
		if !containsType(raw, "event_msg") {
			continue
		}
		var line codexLine
		if json.Unmarshal(raw, &line) != nil || line.Type != "event_msg" {
			continue
		}
		role, text := codexEventText(line.Payload)
		if role == "" || text == "" {
			continue
		}
		lines = append(lines, SearchableLine{
			Text:      text,
			Role:      role,
			Timestamp: line.Timestamp,
			LineNum:   lineNum,
		})
	}
	return lines, sc.Err()
}

// EnrichCodexSession does a full scan of a rollout to extract metrics.
func EnrichCodexSession(path string) (*EnrichmentData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data := &EnrichmentData{}
	toolSet := make(map[string]bool)
	fileSet := make(map[string]bool)

	sc := codexScanner(f)
	for sc.Scan() {
		raw := sc.Bytes()
		var line codexLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}

		switch line.Type {
		case "turn_context":
			var tc codexTurnContext
			if json.Unmarshal(line.Payload, &tc) == nil && tc.Model != "" {
				data.Model = tc.Model
			}

		case "response_item":
			var pt codexPayloadType
			if json.Unmarshal(line.Payload, &pt) != nil || pt.Type != "function_call" {
				continue
			}
			var fc codexFunctionCall
			if json.Unmarshal(line.Payload, &fc) == nil && fc.Name != "" {
				toolSet[fc.Name] = true
			}

		case "event_msg":
			var pt codexPayloadType
			if json.Unmarshal(line.Payload, &pt) != nil {
				continue
			}
			switch pt.Type {
			case "user_message", "agent_message":
				data.MessageCount++
			case "token_count":
				// Codex reports running totals, so the last line wins rather
				// than accumulating across turns.
				var tc codexTokenCount
				if json.Unmarshal(line.Payload, &tc) != nil || tc.Info == nil || tc.Info.Total == nil {
					continue
				}
				t := tc.Info.Total
				data.TotalInputTokens = t.InputTokens
				data.TotalOutputTokens = t.OutputTokens
				data.CacheReadTokens = t.CachedInputTokens
				data.CacheWriteTokens = t.CacheWriteTokens
			case "patch_apply_end":
				var pa codexPatchApply
				if json.Unmarshal(line.Payload, &pa) != nil {
					continue
				}
				for name := range pa.Changes {
					fileSet[name] = true
				}
			}
		}
	}

	data.Model = ShortenModel(data.Model)
	for tool := range toolSet {
		data.ToolsUsed = append(data.ToolsUsed, tool)
	}
	for file := range fileSet {
		data.FilesModified = append(data.FilesModified, file)
	}
	return data, sc.Err()
}
