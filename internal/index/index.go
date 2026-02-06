package index

import (
	"sort"

	"github.com/leon/claude-sessions/internal/session"
)

// Load returns all sessions, using cache when valid, falling back to full scan.
func Load() ([]session.SessionEntry, error) {
	// Try cache first
	cache := LoadCache()
	if IsCacheValid(cache) {
		return cache.Sessions, nil
	}

	// Full scan
	sessions, err := ScanAll()
	if err != nil {
		return nil, err
	}

	// Sort by modified time, newest first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})

	// Save cache in background (best effort)
	go SaveCache(sessions)

	return sessions, nil
}
