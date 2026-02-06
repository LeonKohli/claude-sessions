package tui

import (
	"runtime"
	"strings"
	"sync"

	"github.com/leon/claude-sessions/internal/session"
	"github.com/leon/claude-sessions/internal/util"
)

// deepSearch performs a parallel grep across all session JSONL files.
func deepSearch(sessions []session.SessionEntry, query string) []DeepMatch {
	query = strings.ToLower(query)
	numWorkers := runtime.NumCPU()

	type job struct {
		session session.SessionEntry
	}

	jobs := make(chan job, len(sessions))
	results := make(chan DeepMatch, len(sessions))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if j.session.IsSidechain {
					continue
				}
				snippets := searchFile(j.session.FullPath, query)
				if len(snippets) > 0 {
					results <- DeepMatch{
						Session:  j.session,
						Snippets: snippets,
					}
				}
			}
		}()
	}

	// Send jobs
	for _, s := range sessions {
		jobs <- job{session: s}
	}
	close(jobs)

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	var matches []DeepMatch
	for r := range results {
		matches = append(matches, r)
	}

	return matches
}

// searchFile searches a single JSONL file for the query string.
func searchFile(path, query string) []string {
	lines, err := session.ReadAllText(path)
	if err != nil {
		return nil
	}

	var snippets []string
	for _, line := range lines {
		lower := strings.ToLower(line.Text)
		idx := strings.Index(lower, query)
		if idx >= 0 {
			// Extract snippet around match
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + len(query) + 40
			if end > len(line.Text) {
				end = len(line.Text)
			}
			snippet := line.Text[start:end]
			snippet = strings.TrimSpace(snippet)
			snippets = append(snippets, "["+line.Role+"] "+util.Truncate(snippet, 120))
		}
	}

	return snippets
}
