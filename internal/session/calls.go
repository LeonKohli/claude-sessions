package session

import (
	"encoding/json"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

// ToolCall records an attempted invocation, not proof that it succeeded.
type ToolCall struct {
	Source         string `json:"source,omitempty"`
	ID             string `json:"id,omitempty"`
	Tool           string `json:"tool"`
	At             string `json:"at,omitempty"`
	Line           int    `json:"line"`
	Input          string `json:"input"`
	InputTruncated bool   `json:"input_truncated,omitempty"`
}

// WalkToolCalls visits recorded inputs in file order until visit returns false.
func WalkToolCalls(p provider.Kind, path string, visit func(ToolCall) bool) error {
	for source, err := range transcriptLines(p, path) {
		if err != nil {
			return err
		}
		raw, lineNum := source.raw, source.number
		var line struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		at := ""
		if !line.Timestamp.IsZero() {
			at = line.Timestamp.UTC().Format(time.RFC3339)
		}
		if p == provider.Codex {
			if line.Type != "response_item" {
				continue
			}
			var call struct {
				Type      string `json:"type"`
				ID        string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Input     string `json:"input"`
			}
			if json.Unmarshal(line.Payload, &call) != nil {
				continue
			}
			var input string
			switch call.Type {
			case "function_call":
				input = call.Arguments
			case "custom_tool_call":
				input = call.Input
			default:
				continue
			}
			if !visit(ToolCall{Source: source.path, ID: call.ID, Tool: call.Name, At: at, Line: lineNum, Input: input}) {
				return nil
			}
		} else {
			if line.Type != "assistant" || line.Message == nil {
				continue
			}
			var blocks []struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(line.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				if b.Type == "tool_use" && !visit(ToolCall{Source: source.path, ID: b.ID, Tool: b.Name, At: at, Line: lineNum, Input: string(b.Input)}) {
					return nil
				}
			}
		}
	}
	return nil
}
