package index

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unicode"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

// SearchHit is one session whose transcript matched a query.
type SearchHit struct {
	Session  session.SessionEntry
	Matches  int
	Snippets []string
}

// SearchSessions greps every transcript in parallel and returns the sessions
// that matched, each with a bounded number of context snippets.
//
// maxSnippets and maxChars bound the result at the source rather than after
// the fact: the caller is usually an agent paying for every returned byte.
func SearchSessions(ctx context.Context, sessions []session.SessionEntry, query string, maxSnippets, maxChars int) ([]SearchHit, error) {
	folded := foldQuery(query)
	if len(folded) == 0 || len(sessions) == 0 {
		return nil, ctx.Err()
	}

	reader := new(session.CodexReader)
	jobs := make(chan session.SessionEntry, len(sessions))
	for _, s := range sessions {
		jobs <- s
	}
	close(jobs)

	results := make(chan SearchHit, len(sessions))
	errors := make(chan error, len(sessions))
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if ctx.Err() != nil {
					return
				}
				hit, err := searchOne(ctx, reader, s, folded, maxSnippets, maxChars)
				if err != nil {
					errors <- fmt.Errorf("session %s: %w", s.SessionID, err)
				} else if hit.Matches > 0 {
					results <- hit
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count := len(errors); count > 0 {
		return nil, fmt.Errorf("could not search %d sessions: %w", count, <-errors)
	}

	hits := make([]SearchHit, 0, len(results))
	for h := range results {
		hits = append(hits, h)
	}
	return hits, nil
}

func searchOne(ctx context.Context, reader *session.CodexReader, s session.SessionEntry, query []rune, maxSnippets, maxChars int) (SearchHit, error) {
	hit := SearchHit{Session: s}
	visit := func(line session.SearchableLine) bool {
		idx := indexFold(line.Text, query)
		if idx < 0 {
			return true
		}
		hit.Matches++
		// Keep counting after the snippet budget is spent: the match count is
		// what ranks a session, so it must reflect the whole transcript.
		if maxSnippets > 0 && len(hit.Snippets) >= maxSnippets {
			return true
		}
		hit.Snippets = append(hit.Snippets, snippet(line.Role, line.Text, idx, len(query), maxChars))
		return true
	}
	var err error
	if s.Provider == provider.Codex {
		err = reader.WalkText(ctx, s.FullPath, visit)
	} else {
		err = session.WalkSearchable(ctx, s.Provider, s.FullPath, visit)
	}
	return hit, err
}

// indexFold reports the rune index of the first case-insensitive match, or -1.
//
// It deliberately avoids strings.Index over a lowercased copy: case folding
// changes UTF-8 byte length in both directions (İ shrinks, Ⱥ grows), so a byte
// offset taken from the folded text does not address the original. Applying one
// to the other silently mis-centres the snippet, and panics outright when the
// folded text is longer. Folding rune-by-rune is 1:1, so indices stay aligned.
func indexFold(text string, query []rune) int {
	if len(query) == 0 {
		return -1
	}
	runes := []rune(text)
	if len(runes) < len(query) {
		return -1
	}
	for i := 0; i <= len(runes)-len(query); i++ {
		matched := true
		for j, q := range query {
			if unicode.ToLower(runes[i+j]) != q {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

// snippet extracts readable context centred on a match, addressed in runes.
func snippet(role, text string, start, queryLen, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 160
	}
	prefix := "[" + role + "] "
	budget := maxChars - len([]rune(prefix))
	if budget < 1 {
		prefix, budget = "", maxChars
	}
	pad := max(0, (budget-queryLen)/2)
	runes := []rune(text)
	from := max(0, min(start-pad, len(runes)-budget))
	to := min(len(runes), from+budget)

	out := strings.TrimSpace(string(runes[from:to]))
	out = strings.Join(strings.Fields(out), " ")
	return prefix + out
}

// foldQuery lowercases a query rune-by-rune to match indexFold's comparison.
func foldQuery(query string) []rune {
	runes := []rune(strings.TrimSpace(query))
	for i, r := range runes {
		runes[i] = unicode.ToLower(r)
	}
	return runes
}
