package index

import (
	"encoding/gob"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/LeonKohli/claude-sessions/internal/util"
)

// cacheVersion invalidates metadata derived by older readers.
const cacheVersion = 8

// CachedIndex holds the cached session data + metadata for invalidation.
type CachedIndex struct {
	Version   int
	ScannedAt time.Time
	// Stores this index was built from. CLAUDE_CONFIG_DIR and CODEX_HOME can
	// repoint the scan at a different set of transcripts while the cache stays
	// at one fixed path, so it must not be reused across such a change.
	Stores   []string
	Sessions []session.SessionEntry
}

// storeFingerprint identifies which transcript stores an index covers.
func storeFingerprint() []string {
	return []string{provider.ClaudeProjectsDir(), provider.CodexSessionsDir(), provider.CodexStateDB()}
}

func sameStores(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// LoadCache reads the gob cache from disk. Returns nil if the cache is absent,
// corrupt, or written by an older layout.
func LoadCache() *CachedIndex {
	path, err := util.CachePath()
	if err != nil {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var ci CachedIndex
	if err := gob.NewDecoder(f).Decode(&ci); err != nil {
		return nil
	}
	if ci.Version != cacheVersion || !sameStores(ci.Stores, storeFingerprint()) {
		return nil
	}
	return &ci
}

// SaveCache writes the index to the gob cache.
func SaveCache(sessions []session.SessionEntry, scannedAt time.Time) error {
	dir, err := util.CacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Join(dir, "index.gob")
	f, err := os.CreateTemp(dir, ".index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	ci := CachedIndex{
		Version:   cacheVersion,
		ScannedAt: scannedAt,
		Stores:    storeFingerprint(),
		Sessions:  sessions,
	}

	if err := gob.NewEncoder(f).Encode(&ci); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// IsCacheValid reports whether either store changed after the scan began.
func IsCacheValid(cache *CachedIndex) bool {
	if cache == nil || cache.ScannedAt.IsZero() {
		return false
	}
	cacheMtime := cache.ScannedAt
	for _, s := range cache.Sessions {
		info, err := os.Stat(s.FullPath)
		if err != nil || info.ModTime().After(cacheMtime) || info.Size() != s.FileSize {
			return false
		}
	}

	return claudeStoreUnchanged(cacheMtime) && codexStoreUnchanged(cacheMtime)
}

func claudeStoreUnchanged(since time.Time) bool {
	return storeUnchanged(provider.ClaudeProjectsDir(), since)
}

func codexStoreUnchanged(since time.Time) bool {
	if db := provider.CodexStateDB(); db != "" {
		for _, path := range []string{db, db + "-wal"} {
			info, err := os.Stat(path)
			if err != nil {
				if !os.IsNotExist(err) {
					return false
				}
				continue
			}
			if info.ModTime().After(since) {
				return false
			}
		}
	}
	return storeUnchanged(provider.CodexSessionsDir(), since) && storeUnchanged(provider.CodexArchivedDir(), since)
}

// Check unlisted files too: empty transcripts can gain their first message.
func storeUnchanged(root string, since time.Time) bool {
	if info, err := os.Stat(root); err != nil {
		return os.IsNotExist(err)
	} else if !info.IsDir() {
		return false
	}
	unchanged := true
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) != ".jsonl" && d.Name() != "sessions-index.json" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(since) {
			unchanged = false
			return filepath.SkipAll
		}
		return nil
	})
	return err == nil && unchanged
}
