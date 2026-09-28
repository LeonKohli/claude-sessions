package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Codex rollout records use {timestamp, type, payload} envelopes. Paginated
// histories can span physical rollouts linked by session_meta.history_base.
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
	Type    string `json:"type"`
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

	read := 0
	for raw, err := range readLines(f) {
		if err != nil {
			return
		}
		read += len(raw)
		if read > headScanBudget {
			return
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
			if role, text := codexEventText(line.Payload); role == "user" {
				prompt = text
				return
			}
		case "response_item":
			if role, text := codexResponseText(line.Payload); role == "user" && strings.TrimSpace(text) != "" {
				prompt = text
				return
			}
		}
	}
	return
}

// ReadHead keeps selected-rollout metadata and derives inherited prompts from its history.
func (r *CodexReader) ReadHead(ctx context.Context, path string) (meta CodexMeta, prompt, model string, ts time.Time, ok bool, err error) {
	meta, prompt, model, ts, ok = ReadCodexHead(path)
	if !ok {
		return
	}
	base, _, _, err := historyHeader(path)
	if err != nil {
		err = fmt.Errorf("%w: %v", ErrHistoryUnavailable, err)
		return
	}
	if base == nil {
		return
	}
	prompt = ""
	err = r.WalkText(ctx, path, func(line SearchableLine) bool {
		if line.Role != "user" {
			return true
		}
		prompt = line.Text
		return false
	})
	return
}

// codexEventText maps an event_msg payload to a conversation turn.
// Returns an empty role for events that are not part of the dialogue.
func codexEventText(payload json.RawMessage) (role, text string) {
	var ev codexTextEvent
	if json.Unmarshal(payload, &ev) != nil {
		return "", ""
	}
	switch ev.Type {
	case "user_message":
		role = "user"
	case "agent_message":
		role = "assistant"
	default:
		return "", ""
	}
	return role, ev.Message
}

func codexResponseText(payload json.RawMessage) (string, string) {
	var msg struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(payload, &msg) != nil || msg.Type != "message" || (msg.Role != "user" && msg.Role != "assistant") {
		return "", ""
	}
	var parts []string
	for _, part := range msg.Content {
		if part.Type == "input_text" || part.Type == "output_text" {
			parts = append(parts, part.Text)
		}
	}
	return msg.Role, strings.Join(parts, "\n")
}

// WalkCodexText prefers response messages; legacy events are used only when
// no response conversation exists, so dual-format rollouts aren't duplicated.
func WalkCodexText(ctx context.Context, path string, visit func(SearchableLine) bool) error {
	return new(CodexReader).WalkText(ctx, path, visit)
}

// WalkText preserves response-message preference without retaining legacy text.
func (r *CodexReader) WalkText(ctx context.Context, path string, visit func(SearchableLine) bool) error {
	segments, err := r.history(ctx, path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHistoryUnavailable, err)
	}
	responses := false
	for _, segment := range segments {
		if !readHistorySegment(ctx, segment, func(source historyLine, readErr error) bool {
			if readErr != nil {
				err = readErr
				return false
			}
			entry, ok := codexSearchLine(source, "response_item")
			if !ok {
				return true
			}
			responses = true
			return visit(entry)
		}) {
			return err
		}
	}
	if responses {
		return nil
	}
	for _, segment := range segments {
		if !readHistorySegment(ctx, segment, func(source historyLine, readErr error) bool {
			if readErr != nil {
				err = readErr
				return false
			}
			entry, ok := codexSearchLine(source, "event_msg")
			return !ok || visit(entry)
		}) {
			return err
		}
	}
	return nil
}

func codexSearchLine(source historyLine, kind string) (SearchableLine, bool) {
	if !mayContainJSONStrings(source.raw, kind) {
		return SearchableLine{}, false
	}
	var line codexLine
	if json.Unmarshal(source.raw, &line) != nil || line.Type != kind {
		return SearchableLine{}, false
	}
	var role, text string
	if kind == "response_item" {
		role, text = codexResponseText(line.Payload)
	} else {
		role, text = codexEventText(line.Payload)
	}
	return SearchableLine{Text: text, Role: role, Timestamp: line.Timestamp, LineNum: source.number, Source: source.path}, role != "" && strings.TrimSpace(text) != ""
}

// EnrichCodexSession does a full scan of a rollout to extract metrics.
func EnrichCodexSession(path string) (*EnrichmentData, error) {
	data := &EnrichmentData{}
	responseMessages := false
	toolSet := make(map[string]bool)
	fileSet := make(map[string]bool)

	for source, err := range codexHistoryLines(context.Background(), path) {
		if err != nil {
			return data, err
		}
		var line codexLine
		if json.Unmarshal(source.raw, &line) != nil {
			continue
		}

		switch line.Type {
		case "turn_context":
			var tc codexTurnContext
			if json.Unmarshal(line.Payload, &tc) == nil && tc.Model != "" {
				data.Model = tc.Model
			}

		case "response_item":
			if role, text := codexResponseText(line.Payload); role != "" && strings.TrimSpace(text) != "" {
				if !responseMessages {
					data.MessageCount = 0
					responseMessages = true
				}
				data.MessageCount++
			}
			var pt codexPayloadType
			if json.Unmarshal(line.Payload, &pt) != nil || (pt.Type != "function_call" && pt.Type != "custom_tool_call") {
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
				if !responseMessages {
					data.MessageCount++
				}
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
			case "patch_apply_end", "item_completed":
				_, changes := codexPatchChanges(line.Payload)
				for name := range changes {
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
	return data, nil
}
