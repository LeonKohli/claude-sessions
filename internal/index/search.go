package index

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/LeonKohli/claude-sessions/internal/session"
)

// SearchHit is one session whose transcript matched a query.
type SearchHit struct {
	Session  session.SessionEntry
	Matches  int
	Snippets []string
}

// SearchSessions updates changed transcripts and searches their persisted text.
//
// maxSnippets and maxChars bound the result at the source rather than after
// the fact: the caller is usually an agent paying for every returned byte.
func SearchSessions(ctx context.Context, sessions []session.SessionEntry, query string, maxSnippets, maxChars int) ([]SearchHit, error) {
	folded := foldQuery(query)
	if len(folded) == 0 || len(sessions) == 0 {
		return nil, ctx.Err()
	}
	lock, err := acquireWork(ctx)
	if err != nil {
		return nil, err
	}
	defer lock.Close()

	db, err := openIndex(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	signatures, err := loadSignatures(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := indexSessions(ctx, db, sessions, signatures); err != nil {
		return nil, err
	}
	if err := wantSessions(ctx, db, sessions); err != nil {
		return nil, err
	}
	queryLen := utf8.RuneCountInString(folded)
	// BLOB matching preserves literal bytes, including NUL, after FTS selection.
	statement := `INSERT INTO matches SELECT m.id, m.transcript
FROM search_messages m JOIN wanted w ON w.key = m.transcript
WHERE instr(CAST(m.folded AS BLOB), CAST(? AS BLOB)) > 0`
	arguments := []any{folded}
	if queryLen >= 3 && !strings.ContainsRune(folded, 0) {
		statement = `INSERT INTO matches SELECT m.id, m.transcript
FROM search_text JOIN search_messages m ON m.id = search_text.rowid
JOIN wanted w ON w.key = m.transcript
WHERE search_text MATCH ? AND instr(CAST(m.folded AS BLOB), CAST(? AS BLOB)) > 0`
		arguments = []any{`"` + strings.ReplaceAll(folded, `"`, `""`) + `"`, folded}
	}
	if _, err := db.ExecContext(ctx, statement, arguments...); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT transcript, count(*) FROM matches GROUP BY transcript")
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*SearchHit)
	for rows.Next() {
		var key string
		hit := new(SearchHit)
		if err := rows.Scan(&key, &hit.Matches); err != nil {
			rows.Close()
			return nil, err
		}
		byKey[key] = hit
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if maxSnippets > 0 {
		for key, hit := range byKey {
			rows, err := db.QueryContext(ctx, `SELECT m.role, m.text FROM matches matched
JOIN search_messages m ON m.id = matched.id WHERE matched.transcript = ?
ORDER BY m.ordinal LIMIT ?`, key, maxSnippets)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var role, text string
				if err := rows.Scan(&role, &text); err != nil {
					rows.Close()
					return nil, err
				}
				normalized := strings.ToLower(text)
				offset := strings.Index(normalized, folded)
				hit.Snippets = append(hit.Snippets, snippet(role, text, utf8.RuneCountInString(normalized[:offset]), queryLen, maxChars))
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	var hits []SearchHit
	for _, s := range sessions {
		if hit := byKey[transcriptKey(s.Provider, s.FullPath)]; hit != nil {
			hits = append(hits, SearchHit{Session: s, Matches: hit.Matches, Snippets: hit.Snippets})
		}
	}
	return hits, nil
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

func foldQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}
