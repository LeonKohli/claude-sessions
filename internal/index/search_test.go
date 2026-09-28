package index

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestSearchReturnsMatchingConversationSnippets(t *testing.T) {
	cases := []struct {
		name, text, query, wantSnippet string
		maxChars                       int
	}{
		{"folded text grows", strings.Repeat("Ⱥ", 60) + "error", "error", "Ⱥerror", 40},
		{"folded text shrinks", strings.Repeat("İ", 60) + "error tail", "error", "İerror tail", 24},
		{"emoji", "🚀🚀🚀 error occurred", "error", "🚀🚀🚀 error occurred", 40},
		{"cjk context", "日本語のテキスト error です", "error", "日本語のテキスト error です", 40},
		{"match at end", "some text error", "error", "some text error", 40},
		{"match at start", "error at the start", "error", "error at the start", 40},
		{"long prefix", strings.Repeat("filler ", 100) + "error", "error", "filler error", 40},
		{"mixed case", "Hello World", "world", "Hello World", 40},
		{"upper case", "HELLO", "hello", "HELLO", 40},
		{"cjk before upper case", "日本 ERROR", "error", "日本 ERROR", 40},
		{"query longer than text", "hi", "a much longer query", "", 40},
		{"absent query", "no match here", "absent", "", 40},
		{"empty text", "", "x", "", 40},
		{"empty query", "anything", "", "", 40},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, err := json.Marshal(c.text)
			if err != nil {
				t.Fatal(err)
			}
			body := `{"type":"user","message":{"role":"user","content":` + string(text) + "}}\n"
			path := filepath.Join(t.TempDir(), "conversation.jsonl")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			sessions := []session.SessionEntry{{Provider: provider.Claude, SessionID: "fixture", FullPath: path}}
			hits, err := SearchSessions(context.Background(), sessions, c.query, 1, c.maxChars)
			if err != nil {
				t.Fatal(err)
			}
			if c.wantSnippet == "" {
				if len(hits) != 0 {
					t.Fatalf("unexpected matches: %+v", hits)
				}
				return
			}
			if len(hits) != 1 || hits[0].Session.SessionID != "fixture" || hits[0].Matches != 1 || len(hits[0].Snippets) != 1 {
				t.Fatalf("search results = %+v", hits)
			}
			if !strings.Contains(hits[0].Snippets[0], c.wantSnippet) {
				t.Errorf("snippet = %q, want original context %q", hits[0].Snippets[0], c.wantSnippet)
			}
		})
	}
}

func TestSearchReportsReadFailureAndCancellation(t *testing.T) {
	sessions := []session.SessionEntry{{SessionID: "missing", FullPath: filepath.Join(t.TempDir(), "missing.jsonl")}}
	if _, err := SearchSessions(context.Background(), sessions, "needle", 1, 80); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing transcript error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SearchSessions(ctx, sessions, "needle", 1, 80); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search error = %v", err)
	}
}

func TestSearchCountsMessagesBeyondSnippetLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	body := strings.Repeat(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"needle"}]}}`+"\n", 5)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	sessions := []session.SessionEntry{{Provider: provider.Codex, SessionID: "fixture", FullPath: path}}
	hits, err := SearchSessions(context.Background(), sessions, "needle", 1, 80)
	if err != nil || len(hits) != 1 || hits[0].Matches != 5 || len(hits[0].Snippets) != 1 {
		t.Fatalf("hits = %v, error = %v", hits, err)
	}
}

func TestSearchSnippetBudgetIncludesLongQueryAndRole(t *testing.T) {
	query := strings.Repeat("日本語", 40)
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	body := `{"type":"assistant","message":{"role":"assistant","content":"prefix ` + query + ` suffix"}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	entries := []session.SessionEntry{{SessionID: "long-query", FullPath: path}}
	for _, budget := range []int{1, 10, 40} {
		hits, err := SearchSessions(context.Background(), entries, query, 1, budget)
		if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "long-query" || hits[0].Matches != 1 || len(hits[0].Snippets) != 1 {
			t.Fatalf("match lost: %+v, %v", hits, err)
		}
		if len([]rune(hits[0].Snippets[0])) > budget {
			t.Errorf("budget %d exceeded: %q", budget, hits[0].Snippets[0])
		}
	}
}
