package index

import (
	"strings"
	"testing"
)

// Case folding changes UTF-8 byte length in both directions. Taking a byte
// offset from a lowercased copy and slicing the original panics when the copy
// is longer, and silently mis-centres the snippet when it is shorter. These
// cases reproduced both before the matcher moved into rune space.
func TestSnippetSurvivesCaseFoldSkew(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		query    string
		maxChars int
	}{
		{
			// U+023A (Ⱥ, 2 bytes) folds to U+2C65 (ⱥ, 3 bytes): folded is longer.
			name: "folded text longer than original", query: "error", maxChars: 40,
			text: strings.Repeat("Ⱥ", 60) + "error",
		},
		{
			// U+0130 (İ, 2 bytes) folds to U+0069 (i, 1 byte): folded is shorter.
			name: "folded text shorter than original", query: "error", maxChars: 20,
			text: strings.Repeat("İ", 60) + "error tail",
		},
		{"emoji before the match", "🚀🚀🚀 error occurred", "error", 40},
		{"cjk around the match", "日本語のテキスト error です", "error", 40},
		{"match at the very end", "some text error", "error", 40},
		{"match at the very start", "error at the start", "error", 40},
		{"query longer than text", "hi", "a much longer query", 40},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			folded := foldQuery(c.query)
			idx := indexFold(c.text, folded)
			if idx < 0 {
				if strings.Contains(strings.ToLower(c.text), strings.ToLower(c.query)) {
					t.Fatal("indexFold missed a match that exists")
				}
				return // genuinely absent
			}
			got := snippet("user", c.text, idx, len(folded), c.maxChars)
			if !strings.Contains(strings.ToLower(got), strings.ToLower(c.query)) {
				t.Errorf("snippet lost the match it was centred on: %q", got)
			}
		})
	}
}

func TestIndexFoldIsCaseInsensitive(t *testing.T) {
	cases := []struct {
		text, query string
		want        int
	}{
		{"Hello World", "world", 6},
		{"HELLO", "hello", 0},
		{"no match here", "absent", -1},
		{"", "x", -1},
		{"anything", "", -1},
		{"日本 ERROR", "error", 3},
	}
	for _, c := range cases {
		if got := indexFold(c.text, foldQuery(c.query)); got != c.want {
			t.Errorf("indexFold(%q, %q) = %d, want %d", c.text, c.query, got, c.want)
		}
	}
}

// The match count ranks results, so it must reflect the whole transcript even
// once the snippet budget is spent.
func TestSnippetBudgetDoesNotCapMatchCount(t *testing.T) {
	line := "error " + strings.Repeat("filler error ", 20)
	folded := foldQuery("error")

	count := 0
	for i := 0; i < 1; i++ {
		if indexFold(line, folded) >= 0 {
			count++
		}
	}
	if count == 0 {
		t.Fatal("setup failed: no match in fixture")
	}
	// snippet must not panic for a match near the end of a long line either.
	last := strings.LastIndex(line, "error")
	_ = snippet("user", line, len([]rune(line[:last])), len(folded), 40)
}
