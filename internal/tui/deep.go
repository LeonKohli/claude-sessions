package tui

import (
	"github.com/LeonKohli/claude-sessions/internal/index"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// deepSearchSnippets bounds how much matched text the preview pane shows.
const (
	deepSearchSnippets = 8
	deepSearchChars    = 120
)

// deepSearch greps every transcript for the query. It shares the CLI's search
// core so the browser and the agent-facing `search` command cannot drift.
func deepSearch(sessions []session.SessionEntry, query string) []DeepMatch {
	hits := index.SearchSessions(sessions, query, deepSearchSnippets, deepSearchChars)

	matches := make([]DeepMatch, 0, len(hits))
	for _, h := range hits {
		matches = append(matches, DeepMatch{Session: h.Session, Snippets: h.Snippets})
	}
	return matches
}
