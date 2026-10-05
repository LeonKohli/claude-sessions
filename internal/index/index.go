package index

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

var ErrStoreUnavailable = errors.New("session store unavailable")

// Load discovers current sessions and reuses unchanged transcript metadata.
// A provider that is not installed contributes nothing.
func Load(kinds []provider.Kind) ([]session.SessionEntry, error) {
	lock, err := acquireWork(context.Background())
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	db, err := openIndex(context.Background())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	signature, signatureErr := listingSignature()
	if signatureErr == nil {
		if entries, ok := loadListing(db, signature); ok {
			return filterKinds(entries, kinds), nil
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cache, err := loadMetadata(tx)
	if err != nil {
		return nil, err
	}

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
	go scan(provider.Claude, func() ([]session.SessionEntry, error) { return scanClaude(cache) })
	go scan(provider.Codex, func() ([]session.SessionEntry, error) { return scanCodex(cache) })
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
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if len(scanErrors) == 0 {
		if err := wantSessions(context.Background(), db, all); err != nil {
			return nil, err
		}
		if err := pruneIndex(db); err != nil {
			return nil, err
		}
		if after, err := listingSignature(); signatureErr == nil && err == nil && after == signature {
			saveListing(db, signature, all)
		}
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
