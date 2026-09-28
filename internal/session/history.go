package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

// CodexReader shares the archive path inventory within one read operation.
// Its zero value is ready for concurrent reads; use a new reader for each operation.
type CodexReader struct {
	pathsOnce sync.Once
	paths     map[string][]string
	pathsErr  error
}

var ErrHistoryUnavailable = errors.New("Codex history unavailable")

type historyPosition struct {
	ThreadID string `json:"thread_id"`
	Ordinal  uint64 `json:"end_ordinal_exclusive"`
	Bytes    int64  `json:"end_byte_offset"`
}

type historySegment struct {
	path  string
	end   *historyPosition
	start uint64
}

type historyLine struct {
	raw    []byte
	path   string
	number int
}

func transcriptLines(p provider.Kind, path string) iter.Seq2[historyLine, error] {
	if p == provider.Codex {
		return codexHistoryLines(context.Background(), path)
	}
	return func(yield func(historyLine, error) bool) {
		readHistorySegment(context.Background(), historySegment{path: path}, yield)
	}
}

// codexHistoryLines preserves physical source locations across bounded ancestors.
func codexHistoryLines(ctx context.Context, path string) iter.Seq2[historyLine, error] {
	return new(CodexReader).historyLines(ctx, path)
}

func (r *CodexReader) historyLines(ctx context.Context, path string) iter.Seq2[historyLine, error] {
	return func(yield func(historyLine, error) bool) {
		segments, err := r.history(ctx, path)
		if err != nil {
			yield(historyLine{}, fmt.Errorf("%w: %v", ErrHistoryUnavailable, err))
			return
		}
		for _, segment := range segments {
			if !readHistorySegment(ctx, segment, yield) {
				return
			}
		}
	}
}

func readHistorySegment(ctx context.Context, segment historySegment, yield func(historyLine, error) bool) bool {
	f, err := os.Open(segment.path)
	if err != nil {
		yield(historyLine{}, err)
		return false
	}
	defer f.Close()
	var reader io.Reader = f
	if segment.end != nil {
		reader = io.LimitReader(f, segment.end.Bytes)
	}
	number := 0
	for raw, err := range readLines(reader) {
		if err != nil {
			yield(historyLine{}, err)
			return false
		}
		if err := ctx.Err(); err != nil {
			yield(historyLine{}, err)
			return false
		}
		number++
		if segment.end != nil {
			var record struct {
				Ordinal *uint64 `json:"ordinal"`
			}
			if json.Unmarshal(raw, &record) != nil || record.Ordinal == nil {
				continue
			}
			if *record.Ordinal < segment.start || *record.Ordinal >= segment.end.Ordinal {
				continue
			}
		}
		if !yield(historyLine{raw: raw, path: segment.path, number: number}, nil) {
			return false
		}
	}
	return true
}

func (r *CodexReader) history(ctx context.Context, path string) ([]historySegment, error) {
	var segments []historySegment
	var end *historyPosition
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasSuffix(path, ".zst") {
			return nil, fmt.Errorf("compressed history rollout %s is not supported", path)
		}
		if seen[path] {
			return nil, fmt.Errorf("cycle at %s", path)
		}
		seen[path] = true
		base, ordinal, headerBytes, err := historyHeader(path)
		if err != nil {
			return nil, err
		}
		segment := historySegment{path: path, end: end}
		if end != nil {
			if ordinal == nil || end.Ordinal <= *ordinal || end.Bytes < headerBytes {
				return nil, fmt.Errorf("invalid history cutoff for %s", path)
			}
			if err := checkHistoryPrefix(path, end.Bytes); err != nil {
				return nil, err
			}
			segment.start = *ordinal + 1
		}
		segments = append(segments, segment)
		if base == nil {
			break
		}
		if ordinal == nil || *ordinal != base.Ordinal || base.ThreadID == "" {
			return nil, fmt.Errorf("invalid history reference in %s", path)
		}
		r.pathsOnce.Do(func() { r.paths, r.pathsErr = historyPaths(ctx) })
		if r.pathsErr != nil {
			return nil, r.pathsErr
		}
		matches := r.paths[base.ThreadID]
		if len(matches) != 1 {
			return nil, fmt.Errorf("history rollout %s has %d local files; need one readable JSONL source", base.ThreadID, len(matches))
		}
		path, end = matches[0], base
	}
	slices.Reverse(segments)
	return segments, nil
}

func historyHeader(path string) (*historyPosition, *uint64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, err
	}
	defer f.Close()
	var bytes int64
	for raw, err := range readLines(f) {
		if err != nil {
			return nil, nil, 0, err
		}
		bytes += int64(len(raw))
		if strings.TrimSpace(string(raw)) == "" {
			continue
		}
		var record struct {
			Type    string  `json:"type"`
			Ordinal *uint64 `json:"ordinal"`
			Payload struct {
				Mode string           `json:"history_mode"`
				Base *historyPosition `json:"history_base"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			continue
		}
		if record.Type != "session_meta" {
			return nil, nil, bytes, nil
		}
		if record.Payload.Mode != "paginated" {
			if record.Payload.Base != nil {
				return nil, nil, bytes, fmt.Errorf("unsupported history mode in %s", path)
			}
			return nil, nil, bytes, nil
		}
		return record.Payload.Base, record.Ordinal, bytes, nil
	}
	return nil, nil, bytes, nil
}

func checkHistoryPrefix(path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var last [1]byte
	if size <= 0 {
		return fmt.Errorf("invalid history byte cutoff for %s", path)
	}
	if _, err := f.ReadAt(last[:], size-1); err != nil {
		return fmt.Errorf("incomplete history prefix in %s: %w", path, err)
	}
	if last[0] != '\n' {
		return fmt.Errorf("history cutoff splits a record in %s", path)
	}
	return nil
}

func historyPaths(ctx context.Context) (map[string][]string, error) {
	paths := make(map[string][]string)
	for _, root := range []string{provider.CodexSessionsDir(), provider.CodexArchivedDir()} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if path == root && os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, "rollout-") || (!strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst")) {
				return nil
			}
			stem := strings.TrimSuffix(strings.TrimSuffix(name, ".zst"), ".jsonl")
			if len(stem) >= 36 {
				id := stem[len(stem)-36:]
				paths[id] = append(paths[id], path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return paths, nil
}
