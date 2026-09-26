package index

import (
	"runtime"
	"strings"
	"sync"
	"unicode"

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
func SearchSessions(sessions []session.SessionEntry, query string, maxSnippets, maxChars int) []SearchHit {
	folded := foldQuery(query)
	if len(folded) == 0 || len(sessions) == 0 {
		return nil
	}

	jobs := make(chan session.SessionEntry, len(sessions))
	for _, s := range sessions {
		jobs <- s
	}
	close(jobs)

	results := make(chan SearchHit, len(sessions))
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if hit, ok := searchOne(s, folded, maxSnippets, maxChars); ok {
					results <- hit
				}
			}
		}()
	}
	wg.Wait()
	close(results)

	hits := make([]SearchHit, 0, len(results))
	for h := range results {
		hits = append(hits, h)
	}
	return hits
}

func searchOne(s session.SessionEntry, query []rune, maxSnippets, maxChars int) (SearchHit, bool) {
	lines, err := session.ReadSearchable(s.Provider, s.FullPath)
	if err != nil {
		return SearchHit{}, false
	}

	hit := SearchHit{Session: s}
	for _, line := range lines {
		idx := indexFold(line.Text, query)
		if idx < 0 {
			continue
		}
		hit.Matches++
		// Keep counting after the snippet budget is spent: the match count is
		// what ranks a session, so it must reflect the whole transcript.
		if maxSnippets > 0 && len(hit.Snippets) >= maxSnippets {
			continue
		}
		hit.Snippets = append(hit.Snippets, snippet(line.Role, line.Text, idx, len(query), maxChars))
	}
	return hit, hit.Matches > 0
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
	pad := maxChars / 2

	runes := []rune(text)
	from := max(0, start-pad)
	to := min(len(runes), start+queryLen+pad)

	out := strings.TrimSpace(string(runes[from:to]))
	out = strings.Join(strings.Fields(out), " ")
	return "[" + role + "] " + out
}

// foldQuery lowercases a query rune-by-rune to match indexFold's comparison.
func foldQuery(query string) []rune {
	runes := []rune(strings.TrimSpace(query))
	for i, r := range runes {
		runes[i] = unicode.ToLower(r)
	}
	return runes
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
