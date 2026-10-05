package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
	"github.com/LeonKohli/claude-sessions/internal/util"
)

// Bump when transcript or metadata extraction changes.
const readerVersion = 1

func openIndex(ctx context.Context) (*sql.DB, error) {
	dir, err := util.CacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "index.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(3000)"}).String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS transcripts (key TEXT PRIMARY KEY, signature TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS search_messages (
 id INTEGER PRIMARY KEY, transcript TEXT NOT NULL, ordinal INTEGER NOT NULL,
 role TEXT NOT NULL, text TEXT NOT NULL, folded TEXT NOT NULL, source TEXT NOT NULL, line INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_transcript ON search_messages(transcript, ordinal);
CREATE VIRTUAL TABLE IF NOT EXISTS search_text USING fts5(
 folded, content='search_messages', content_rowid='id', tokenize='trigram case_sensitive 1'
);
CREATE TRIGGER IF NOT EXISTS messages_insert AFTER INSERT ON search_messages BEGIN
 INSERT INTO search_text(rowid, folded) VALUES (new.id, new.folded);
END;
CREATE TRIGGER IF NOT EXISTS messages_delete AFTER DELETE ON search_messages BEGIN
 INSERT INTO search_text(search_text, rowid, folded) VALUES ('delete', old.id, old.folded);
END;
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, signature TEXT NOT NULL, entry BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS listing (id INTEGER PRIMARY KEY CHECK(id = 1), signature TEXT NOT NULL, entries BLOB NOT NULL);
CREATE TEMP TABLE wanted (key TEXT PRIMARY KEY);
CREATE TEMP TABLE matches (id INTEGER PRIMARY KEY, transcript TEXT NOT NULL);
CREATE INDEX matches_transcript ON matches(transcript);
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func pruneIndex(db *sql.DB) error {
	rows, err := db.Query(`SELECT key FROM transcripts WHERE key NOT IN (SELECT key FROM wanted)
UNION SELECT key FROM metadata WHERE key NOT IN (SELECT key FROM wanted)`)
	if err != nil {
		return err
	}
	var removed []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		removed = append(removed, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(removed) == 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, key := range removed {
		if _, err := tx.Exec("DELETE FROM search_messages WHERE transcript = ?", key); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM transcripts WHERE key = ?", key); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM metadata WHERE key = ?", key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func transcriptKey(kind provider.Kind, path string) string {
	return kind.String() + ":" + path
}

func transcriptSignature(ctx context.Context, reader *session.CodexReader, kind provider.Kind, path, saved string) (string, error) {
	signature, err := fileSignature(ctx, []string{path})
	if err != nil || kind != provider.Codex || signature == saved {
		return signature, err
	}
	paths, err := reader.HistoryFiles(ctx, path)
	if err != nil {
		return "", err
	}
	if len(paths) == 1 {
		return signature, nil
	}
	return fileSignature(ctx, paths)
}

func fileSignature(ctx context.Context, paths []string) (string, error) {
	type stamp struct {
		Path           string
		Size, Modified int64
	}
	stamps := make([]stamp, 0, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		stamps = append(stamps, stamp{p, info.Size(), info.ModTime().UnixNano()})
	}
	data, err := json.Marshal(stamps)
	return strconv.Itoa(readerVersion) + ":" + string(data), err
}

type savedMetadata struct {
	signature string
	entry     []byte
}

type metadataCache struct {
	tx      *sql.Tx
	entries map[string]savedMetadata
}

func loadMetadata(tx *sql.Tx) (*metadataCache, error) {
	rows, err := tx.Query("SELECT key, signature, entry FROM metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cache := &metadataCache{tx: tx, entries: make(map[string]savedMetadata)}
	for rows.Next() {
		var key string
		var saved savedMetadata
		if err := rows.Scan(&key, &saved.signature, &saved.entry); err != nil {
			return nil, err
		}
		cache.entries[key] = saved
	}
	return cache, rows.Err()
}

func loadSignatures(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT key, signature FROM transcripts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	signatures := make(map[string]string)
	for rows.Next() {
		var key, signature string
		if err := rows.Scan(&key, &signature); err != nil {
			return nil, err
		}
		signatures[key] = signature
	}
	return signatures, rows.Err()
}

func wantSessions(ctx context.Context, db *sql.DB, sessions []session.SessionEntry) error {
	keys := make([]string, len(sessions))
	for i, entry := range sessions {
		keys[i] = transcriptKey(entry.Provider, entry.FullPath)
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO wanted(key) SELECT value FROM json_each(?)", string(data))
	return err
}

func cachedClaude(cache *metadataCache, job claudeScanJob) (session.SessionEntry, error) {
	ctx := context.Background()
	signature, err := transcriptSignature(ctx, nil, provider.Claude, job.path, "")
	if err != nil {
		return session.SessionEntry{}, err
	}
	contextKey, _ := json.Marshal([]string{job.originalPath, job.dirName})
	signature += string(contextKey)
	key := transcriptKey(provider.Claude, job.path)
	var entry session.SessionEntry
	if saved, ok := cache.entries[key]; ok && saved.signature == signature && json.Unmarshal(saved.entry, &entry) == nil {
		return entry, nil
	}
	entry, err = scanJSONLFile(job.path, job.originalPath, job.dirName)
	if err == nil && entry.FirstPrompt != "" {
		after, statErr := transcriptSignature(ctx, nil, provider.Claude, job.path, "")
		if statErr == nil && after+string(contextKey) == signature {
			data, marshalErr := json.Marshal(entry)
			if marshalErr == nil {
				cache.tx.Exec("INSERT OR REPLACE INTO metadata(key, signature, entry) VALUES (?, ?, ?)", key, signature, data)
			}
		}
	}
	return entry, err
}

type codexHead struct {
	Meta          session.CodexMeta
	Prompt, Model string
	Timestamp     time.Time
}

func readCodexHead(cache *metadataCache, reader *session.CodexReader, path string) (session.CodexMeta, string, string, time.Time, bool, error) {
	ctx := context.Background()
	key := transcriptKey(provider.Codex, path)
	signature, err := transcriptSignature(ctx, reader, provider.Codex, path, cache.entries[key].signature)
	if err != nil {
		return session.CodexMeta{}, "", "", time.Time{}, false, err
	}
	var head codexHead
	if saved, ok := cache.entries[key]; ok && saved.signature == signature && json.Unmarshal(saved.entry, &head) == nil {
		return head.Meta, head.Prompt, head.Model, head.Timestamp, true, nil
	}
	meta, prompt, model, ts, ok, err := reader.ReadHead(ctx, path)
	if err == nil && ok && prompt != "" {
		after, statErr := transcriptSignature(ctx, reader, provider.Codex, path, "")
		if statErr == nil && after == signature {
			data, marshalErr := json.Marshal(codexHead{meta, prompt, model, ts})
			if marshalErr == nil {
				cache.tx.Exec("INSERT OR REPLACE INTO metadata(key, signature, entry) VALUES (?, ?, ?)", key, signature, data)
			}
		}
	}
	return meta, prompt, model, ts, ok, err
}

func indexTranscript(ctx context.Context, db *sql.DB, reader *session.CodexReader, s session.SessionEntry, saved string) error {
	key := transcriptKey(s.Provider, s.FullPath)
	for range 3 {
		signature, err := transcriptSignature(ctx, reader, s.Provider, s.FullPath, saved)
		if err != nil {
			return err
		}
		if saved == signature {
			return nil
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		err = syncTranscript(ctx, tx, reader, s, key)
		if err != nil {
			tx.Rollback()
			return err
		}
		after, err := transcriptSignature(ctx, reader, s.Provider, s.FullPath, "")
		if err != nil {
			tx.Rollback()
			return err
		}
		if after != signature {
			tx.Rollback()
			continue
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO transcripts(key, signature) VALUES (?, ?)", key, signature); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return fmt.Errorf("transcript keeps changing while indexing: %s", s.FullPath)
}

func syncTranscript(ctx context.Context, tx *sql.Tx, reader *session.CodexReader, s session.SessionEntry, key string) error {
	type indexedMessage struct {
		id                 int64
		role, source, text string
		line               int
	}
	rows, err := tx.QueryContext(ctx, "SELECT id, ordinal, role, text, source, line FROM search_messages WHERE transcript = ?", key)
	if err != nil {
		return err
	}
	previous := make(map[int]indexedMessage)
	for rows.Next() {
		var ordinal int
		var message indexedMessage
		if err := rows.Scan(&message.id, &ordinal, &message.role, &message.text, &message.source, &message.line); err != nil {
			rows.Close()
			return err
		}
		previous[ordinal] = message
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, "INSERT INTO search_messages(transcript, ordinal, role, text, folded, source, line) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer insert.Close()
	ordinal := 0
	var writeErr error
	visit := func(line session.SearchableLine) bool {
		ordinal++
		if old, ok := previous[ordinal]; ok {
			if old.text == line.Text && old.role == line.Role && old.source == line.Source && old.line == line.LineNum {
				return true
			}
			if _, writeErr = tx.ExecContext(ctx, "DELETE FROM search_messages WHERE id = ?", old.id); writeErr != nil {
				return false
			}
		}
		_, writeErr = insert.ExecContext(ctx, key, ordinal, line.Role, line.Text, strings.ToLower(line.Text), line.Source, line.LineNum)
		return writeErr == nil
	}
	if s.Provider == provider.Codex {
		err = reader.WalkText(ctx, s.FullPath, visit)
	} else {
		err = session.WalkSearchable(ctx, s.Provider, s.FullPath, visit)
	}
	if err != nil {
		return err
	}
	if writeErr != nil {
		return writeErr
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM search_messages WHERE transcript = ? AND ordinal > ?", key, ordinal)
	return err
}
