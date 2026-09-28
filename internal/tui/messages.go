package tui

import (
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// SessionsLoadedMsg is sent when the session index is ready.
type SessionsLoadedMsg struct {
	Sessions []session.SessionEntry
}

// PreviewLoadedMsg is sent when session preview data is ready.
type PreviewLoadedMsg struct {
	SessionID  string
	Messages   []session.PreviewMessage
	Enrichment *session.EnrichmentData
}

// DeepSearchResultMsg carries results from deep content search.
type DeepSearchResultMsg struct {
	Query   string
	Results []DeepMatch
	Err     error
}

// DeepMatch is a session that matched a deep search.
type DeepMatch struct {
	Session  session.SessionEntry
	Snippets []string
}

// ToastMsg shows a temporary notification.
type ToastMsg struct {
	Text string
}

// ClearToastMsg clears the toast notification.
type ClearToastMsg struct{}

// SearchDebounceMsg triggers a deep search after debounce timeout.
type SearchDebounceMsg struct {
	Query string
	ID    int // debounce generation to ignore stale triggers
}
