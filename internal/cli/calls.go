package cli

import (
	"io"
	"strings"

	"github.com/LeonKohli/claude-sessions/internal/session"
)

// Calls filters complete tool inputs before applying result and excerpt limits.
func Calls(e session.SessionEntry, query string, limit, maxChars int) (Result, error) {
	query = strings.TrimSpace(query)
	folded := strings.ToLower(query)
	calls := make([]session.ToolCall, 0)
	err := session.WalkToolCalls(e.Provider, e.FullPath, func(call session.ToolCall) bool {
		if !strings.Contains(strings.ToLower(call.ID+"\n"+call.Tool+"\n"+call.Input), folded) {
			return true
		}
		call.Input, call.InputTruncated = textExcerpt(call.Input, folded, maxChars)
		calls = append(calls, call)
		return len(calls) <= limit
	})
	if err != nil {
		return Result{}, err
	}
	shown, trunc := boundLazy(calls, limit)
	if trunc != nil {
		trunc.Hint = "more matching calls exist; narrow the query or raise --limit (total not counted)"
	}
	return Result{
		Data:      map[string]any{"session": toRef(e), "query": query, "calls": shown, "count": len(shown)},
		Truncated: trunc,
		Text: func(w io.Writer) {
			p := printer{w}
			for _, call := range shown {
				p.line("%s  %s  line %d  %s", call.ID, call.Tool, call.Line, call.At)
				p.line("%s", call.Input)
				if call.InputTruncated {
					p.line("(input excerpt; use --max-chars 0 for the full recorded input)")
				}
				p.blank()
			}
		},
	}, nil
}
