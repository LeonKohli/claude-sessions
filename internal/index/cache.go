package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

const listingVersion = 1

func listingSignature() (string, error) {
	hash := sha256.New()
	fmt.Fprintln(hash, readerVersion, listingVersion)
	for _, root := range []string{provider.ClaudeProjectsDir(), provider.CodexSessionsDir(), provider.CodexArchivedDir()} {
		fmt.Fprintln(hash, root)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if path == root && os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if path == root && !entry.IsDir() {
				return fmt.Errorf("%s is not a directory", root)
			}
			if !entry.IsDir() && !strings.HasSuffix(path, ".jsonl") && !strings.HasSuffix(path, ".jsonl.zst") && entry.Name() != "sessions-index.json" {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "%q %d %d\n", path, info.Size(), info.ModTime().UnixNano())
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	db := provider.CodexStateDB()
	for _, path := range []string{db, db + "-wal"} {
		fmt.Fprintln(hash, path)
		if db == "" {
			continue
		}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%d %d\n", info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func loadListing(db *sql.DB, signature string) ([]session.SessionEntry, bool) {
	var data []byte
	if db.QueryRow("SELECT entries FROM listing WHERE id = 1 AND signature = ?", signature).Scan(&data) != nil {
		return nil, false
	}
	var entries []session.SessionEntry
	if json.Unmarshal(data, &entries) != nil {
		return nil, false
	}
	return entries, true
}

func saveListing(db *sql.DB, signature string, entries []session.SessionEntry) {
	data, err := json.Marshal(entries)
	if err == nil {
		db.Exec("INSERT OR REPLACE INTO listing(id, signature, entries) VALUES (1, ?, ?)", signature, data)
	}
}
