package index

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

var ErrStoreUnavailable = errors.New("session store unavailable")

// Load returns sessions from every requested provider, using the cache when
// valid and falling back to a full scan. A provider that is not installed
// contributes nothing rather than failing the load.
func Load(kinds []provider.Kind) ([]session.SessionEntry, error) {
	if cache := LoadCache(); IsCacheValid(cache) {
		return filterKinds(cache.Sessions, kinds), nil
	}

	scannedAt := time.Now()
	var (
		mu         sync.Mutex
		all        []session.SessionEntry
		wg         sync.WaitGroup
		scanErrors = make(map[provider.Kind]error)
	)
	scan := func(kind provider.Kind, fn func() ([]session.SessionEntry, error)) {
		defer wg.Done()
		entries, err := fn()
		mu.Lock()
		if err != nil {
			scanErrors[kind] = err
		}
		all = append(all, entries...)
		mu.Unlock()
	}

	wg.Add(2)
	go scan(provider.Claude, ScanClaude)
	go scan(provider.Codex, ScanCodex)
	wg.Wait()
	if len(kinds) == 0 {
		kinds = provider.All
	}
	for _, kind := range kinds {
		if err := scanErrors[kind]; err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrStoreUnavailable, kind, err)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Modified.After(all[j].Modified)
	})

	// Write the cache before returning. A background save is lost whenever the
	// process is short-lived, which is every non-interactive invocation: each
	// one would re-scan the whole store and leave an orphaned .tmp behind.
	// Encoding costs ~30ms against a ~2s scan, so it is not worth deferring.
	//
	// The full index is cached regardless of the requested filter, so a later
	// run with different flags still hits warm.
	if len(scanErrors) == 0 {
		_ = SaveCache(all, scannedAt)
	}

	return filterKinds(all, kinds), nil
}

func filterKinds(sessions []session.SessionEntry, kinds []provider.Kind) []session.SessionEntry {
	if len(kinds) == 0 || len(kinds) == len(provider.All) {
		return sessions
	}
	want := make(map[provider.Kind]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	out := make([]session.SessionEntry, 0, len(sessions))
	for _, s := range sessions {
		if want[s.Provider] {
			out = append(out, s)
		}
	}
	return out
}
