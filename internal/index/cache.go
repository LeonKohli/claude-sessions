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

// cacheVersion invalidates the gob whenever the entry layout or its derived
// values change. Gob tolerates added fields silently, so bumping this is the
// only thing that stops a stale cache serving entries built by older rules —
// v3 added Parent and widened ShortID from 8 to 12 characters.
const cacheVersion = 3

// CachedIndex holds the cached session data + metadata for invalidation.
type CachedIndex struct {
	Version int
	// Stores this index was built from. CLAUDE_CONFIG_DIR and CODEX_HOME can
	// repoint the scan at a different set of transcripts while the cache stays
	// at one fixed path, so it must not be reused across such a change.
	Stores   []string
	Sessions []session.SessionEntry
	// Map fullPath -> fileMtime for incremental invalidation
	MtimeMap map[string]int64
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
func SaveCache(sessions []session.SessionEntry) error {
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

	mtimeMap := make(map[string]int64, len(sessions))
	for _, s := range sessions {
		mtimeMap[s.FullPath] = s.FileMtime
	}

	ci := CachedIndex{
		Version:  cacheVersion,
		Stores:   storeFingerprint(),
		Sessions: sessions,
		MtimeMap: mtimeMap,
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

// IsCacheValid reports whether either provider's store has changed since the
// cache was written.
func IsCacheValid(cache *CachedIndex) bool {
	if cache == nil {
		return false
	}
	path, err := util.CachePath()
	if err != nil {
		return false
	}
	cacheInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	cacheMtime := cacheInfo.ModTime()
	for _, s := range cache.Sessions {
		info, err := os.Stat(s.FullPath)
		if err != nil || info.ModTime().After(cacheMtime) || info.Size() != s.FileSize {
			return false
		}
	}

	return claudeStoreUnchanged(cacheMtime) && codexStoreUnchanged(cacheMtime)
}

func claudeStoreUnchanged(since time.Time) bool {
	projectsDir := provider.ClaudeProjectsDir()
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		// No Claude install: nothing that can go stale.
		return os.IsNotExist(err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return false
		}
		if info.ModTime().After(since) {
			return false
		}

		idxPath := filepath.Join(projectsDir, entry.Name(), "sessions-index.json")
		if idxInfo, err := os.Stat(idxPath); err == nil {
			if idxInfo.ModTime().After(since) {
				return false
			}
		}
	}

	return true
}

// codexStoreUnchanged checks the state database first — it is rewritten on
// every thread update — then the date-partitioned rollout directories, which
// is a few hundred stats rather than a walk over every transcript.
func codexStoreUnchanged(since time.Time) bool {
	if db := provider.CodexStateDB(); db != "" {
		for _, p := range []string{db, db + "-wal"} {
			if info, err := os.Stat(p); err == nil && info.ModTime().After(since) {
				return false
			}
		}
	}

	root := provider.CodexSessionsDir()
	if _, err := os.Stat(root); err != nil {
		return true // no Codex install
	}

	unchanged := true
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(since) {
			unchanged = false
			return filepath.SkipAll
		}
		return nil
	})
	return unchanged
}
