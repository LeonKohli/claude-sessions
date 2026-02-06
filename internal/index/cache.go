package index

import (
	"encoding/gob"
	"os"
	"path/filepath"

	"github.com/leon/claude-sessions/internal/session"
	"github.com/leon/claude-sessions/internal/util"
)

// CachedIndex holds the cached session data + metadata for invalidation.
type CachedIndex struct {
	Sessions []session.SessionEntry
	// Map fullPath -> fileMtime for incremental invalidation
	MtimeMap map[string]int64
}

// LoadCache reads the gob cache from disk. Returns nil if cache doesn't exist or is corrupt.
func LoadCache() *CachedIndex {
	path := util.CachePath()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var ci CachedIndex
	if err := gob.NewDecoder(f).Decode(&ci); err != nil {
		return nil
	}
	return &ci
}

// SaveCache writes the index to the gob cache.
func SaveCache(sessions []session.SessionEntry) error {
	dir := util.CacheDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := util.CachePath()
	tmpPath := path + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	mtimeMap := make(map[string]int64, len(sessions))
	for _, s := range sessions {
		mtimeMap[s.FullPath] = s.FileMtime
	}

	ci := CachedIndex{
		Sessions: sessions,
		MtimeMap: mtimeMap,
	}

	if err := gob.NewEncoder(f).Encode(&ci); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	f.Close()

	return os.Rename(tmpPath, path)
}

// IsCacheValid checks if the cached index is still valid by checking
// if the projects directory has been modified since the cache was written.
func IsCacheValid(cache *CachedIndex) bool {
	if cache == nil {
		return false
	}

	// Quick check: compare projects dir mtime
	projectsDir := util.ClaudeProjectsDir()
	cacheInfo, err := os.Stat(util.CachePath())
	if err != nil {
		return false
	}

	// Check if any project directory is newer than cache
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return false
	}

	cacheMtime := cacheInfo.ModTime()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return false
		}
		if info.ModTime().After(cacheMtime) {
			return false
		}

		// Check if sessions-index.json is newer
		idxPath := filepath.Join(dirPath, "sessions-index.json")
		if idxInfo, err := os.Stat(idxPath); err == nil {
			if idxInfo.ModTime().After(cacheMtime) {
				return false
			}
		}
	}

	return true
}
