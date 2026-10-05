package tui

import (
	"context"
	"github.com/LeonKohli/claude-sessions/internal/index"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// deepSearchSnippets bounds how much matched text the preview pane shows.
const (
	deepSearchSnippets = 8
	deepSearchChars    = 120
)

// deepSearch shares the CLI's indexed conversation search.
func deepSearch(ctx context.Context, sessions []session.SessionEntry, query string) ([]DeepMatch, error) {
	hits, err := index.SearchSessions(ctx, sessions, query, deepSearchSnippets, deepSearchChars)
	if err != nil {
		return nil, err
	}

	matches := make([]DeepMatch, 0, len(hits))
	for _, h := range hits {
		matches = append(matches, DeepMatch{Session: h.Session, Snippets: h.Snippets})
	}
	return matches, nil
}
