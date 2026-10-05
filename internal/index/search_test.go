package index

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/provider"
	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestSearchReturnsMatchingConversationSnippets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
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
		{"literal quotes", `foo"bar`, `foo"bar`, `foo"bar`, 40},
		{"literal wildcards", "a%b a_b", "a%b", "a%b a_b", 40},
		{"short unicode query", "中文搜索", "中文", "中文搜索", 40},
		{"Go unicode lowercasing", "İstanbul", "istan", "İstanbul", 40},
		{"Cherokee lowercasing", "xᎠx", "xꭰx", "xᎠx", 40},
		{"NUL literal", "a\x00bc", "a\x00b", "a\x00bc", 40},
		{"NUL is not adjacent text", "a\x00bc", "abc", "", 40},
		{"noncharacters stay distinct", "x\ufffex", "x\ufffdx", "", 40},
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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
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

func TestSearchWithoutSnippetsStillCountsMatchingMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, kind := range provider.All {
		t.Run(kind.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "conversation.jsonl")
			line := `{"type":"user","message":{"role":"user","content":"needle"}}`
			if kind == provider.Codex {
				line = `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"needle"}]}}`
			}
			if err := os.WriteFile(path, []byte(strings.Repeat(line+"\n", 5)), 0600); err != nil {
				t.Fatal(err)
			}
			entries := []session.SessionEntry{{Provider: kind, SessionID: "fixture", FullPath: path}}
			hits, err := SearchSessions(context.Background(), entries, "needle", 0, 80)
			if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "fixture" || hits[0].Matches != 5 {
				t.Fatalf("matches lost: %+v, %v", hits, err)
			}
			if len(hits[0].Snippets) != 0 {
				t.Fatalf("zero snippet budget returned %d snippets", len(hits[0].Snippets))
			}
		})
	}
}

func BenchmarkSearchTranscripts(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	for _, kind := range provider.All {
		b.Run(kind.String(), func(b *testing.B) {
			root := b.TempDir()
			text := strings.Repeat("ordinary conversation text ", 160) + "needle"
			line := `{"type":"assistant","message":{"role":"assistant","content":"` + text + `"}}`
			if kind == provider.Codex {
				line = `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + text + `"}]}}`
			}
			body := []byte(strings.Repeat(line+"\n", 128))
			var entries []session.SessionEntry
			for i := range 32 {
				path := filepath.Join(root, fmt.Sprintf("%d.jsonl", i))
				if err := os.WriteFile(path, body, 0600); err != nil {
					b.Fatal(err)
				}
				entries = append(entries, session.SessionEntry{Provider: kind, FullPath: path})
			}
			b.SetBytes(int64(len(body) * len(entries)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				hits, err := SearchSessions(context.Background(), entries, "needle", 0, 80)
				if err != nil || len(hits) != 32 || hits[0].Matches != 128 {
					b.Fatalf("search failed: %d hits, %v", len(hits), err)
				}
			}
		})
	}
}

func TestSearchSnippetBudgetIncludesLongQueryAndRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
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

func TestSearchUpdatesPreviouslyIndexedTranscript(t *testing.T) {
	for _, change := range []string{"append", "replace", "truncate", "delete"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "conversation.jsonl")
			old := `{"type":"user","message":{"role":"user","content":"old needle"}}` + "\n"
			if err := os.WriteFile(path, []byte(old), 0600); err != nil {
				t.Fatal(err)
			}
			entries := []session.SessionEntry{{SessionID: "changing", FullPath: path}}
			if hits, err := SearchSessions(context.Background(), entries, "needle", 1, 80); err != nil || len(hits) != 1 || hits[0].Matches != 1 {
				t.Fatalf("initial search = %+v, %v", hits, err)
			}
			body := `{"type":"user","message":{"role":"user","content":"new needle"}}`
			want := 1
			switch change {
			case "append":
				body = old + body
				want = 2
			case "replace":
				body += "\n"
			case "truncate":
				body, want = "", 0
			case "delete":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if change != "delete" {
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				changed := time.Now().Add(time.Second)
				if err := os.Chtimes(path, changed, changed); err != nil {
					t.Fatal(err)
				}
			}
			hits, err := SearchSessions(context.Background(), entries, "needle", 1, 80)
			if change == "delete" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("deleted transcript returned stale hits: %+v, %v", hits, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want == 0 {
				if len(hits) != 0 {
					t.Fatalf("truncated transcript returned stale hits: %+v", hits)
				}
				return
			}
			if len(hits) != 1 || hits[0].Session.SessionID != "changing" || hits[0].Matches != want || len(hits[0].Snippets) != 1 {
				t.Fatalf("updated search = %+v", hits)
			}
			if change == "replace" && !strings.Contains(hits[0].Snippets[0], "new needle") {
				t.Fatalf("replacement returned stale text: %+v", hits)
			}
		})
	}
}

func TestSearchDropsLegacyCodexTextWhenResponseMessagesAppear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	legacy := `{"type":"event_msg","payload":{"type":"user_message","message":"legacy question"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"agent_message","message":"legacy answer"}}` + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	entries := []session.SessionEntry{{Provider: provider.Codex, SessionID: "format-change", FullPath: path}}
	if hits, err := SearchSessions(context.Background(), entries, "legacy", 2, 80); err != nil || len(hits) != 1 || hits[0].Matches != 2 {
		t.Fatalf("initial legacy conversation = %+v, %v", hits, err)
	}
	response := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"canonical question"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(legacy+response), 0600); err != nil {
		t.Fatal(err)
	}
	if hits, err := SearchSessions(context.Background(), entries, "legacy", 2, 80); err != nil || len(hits) != 0 {
		t.Fatalf("obsolete legacy conversation = %+v, %v", hits, err)
	}
	hits, err := SearchSessions(context.Background(), entries, "canonical", 2, 80)
	if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "format-change" || hits[0].Matches != 1 || len(hits[0].Snippets) != 1 || !strings.Contains(hits[0].Snippets[0], "canonical question") {
		t.Fatalf("canonical conversation = %+v, %v", hits, err)
	}
}

func BenchmarkSearchAfterAppend(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	for _, kind := range provider.All {
		b.Run(kind.String(), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "conversation.jsonl")
			text := strings.Repeat("ordinary conversation text ", 80) + "needle"
			line := `{"type":"user","message":{"role":"user","content":"` + text + `"}}` + "\n"
			if kind == provider.Codex {
				line = `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}}` + "\n"
			}
			if err := os.WriteFile(path, []byte(strings.Repeat(line, 2048)), 0600); err != nil {
				b.Fatal(err)
			}
			entries := []session.SessionEntry{{Provider: kind, SessionID: "growing", FullPath: path}}
			if _, err := SearchSessions(context.Background(), entries, "needle", 0, 80); err != nil {
				b.Fatal(err)
			}
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				b.Fatal(err)
			}
			defer file.Close()
			matches := 2048
			b.ResetTimer()
			for b.Loop() {
				if _, err := file.WriteString(line); err != nil {
					b.Fatal(err)
				}
				matches++
				hits, err := SearchSessions(context.Background(), entries, "needle", 0, 80)
				if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "growing" || hits[0].Matches != matches {
					b.Fatalf("appended conversation = %+v, %v", hits, err)
				}
			}
		})
	}
}

func TestSearchAcrossManyTranscriptsReportsEarliestFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	var entries []session.SessionEntry
	for i := range 40 {
		path := filepath.Join(root, fmt.Sprintf("%02d.jsonl", i))
		if i != 7 && i != 31 {
			line := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"needle in session %02d"}}`+"\n", i)
			if err := os.WriteFile(path, []byte(line), 0600); err != nil {
				t.Fatal(err)
			}
		}
		entries = append(entries, session.SessionEntry{Provider: provider.Claude, SessionID: fmt.Sprintf("s%02d", i), FullPath: path})
	}
	_, err := SearchSessions(context.Background(), entries, "needle", 1, 80)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "session s07:") {
		t.Fatalf("error = %v, want the missing transcript of s07", err)
	}
	present := slices.DeleteFunc(slices.Clone(entries), func(s session.SessionEntry) bool { return s.SessionID == "s07" || s.SessionID == "s31" })
	hits, err := SearchSessions(context.Background(), present, "needle", 1, 80)
	if err != nil || len(hits) != len(present) {
		t.Fatalf("hits = %d, %v; want %d", len(hits), err, len(present))
	}
	for i, hit := range hits {
		want := "needle in session " + strings.TrimPrefix(present[i].SessionID, "s")
		if hit.Session.SessionID != present[i].SessionID || hit.Matches != 1 || len(hit.Snippets) != 1 || !strings.Contains(hit.Snippets[0], want) {
			t.Fatalf("hit %d = %+v, want %q", i, hit, want)
		}
	}
}

// A process can stop between storing bulk-loaded messages and indexing them.
// The stored transcript is current, so only the index recovery makes it searchable.
func TestSearchFindsMessagesFromInterruptedBulkLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"needle after restart"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := session.SessionEntry{Provider: provider.Claude, SessionID: "interrupted", FullPath: path}
	db, err := openIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := bulkLoadSearchText(db); err != nil {
		t.Fatal(err)
	}
	parsed := parseTranscript(context.Background(), new(session.CodexReader), entry, "")
	parsed.key = transcriptKey(entry.Provider, entry.FullPath)
	tx, err := db.Begin()
	if err != nil || parsed.err != nil {
		t.Fatal(err, parsed.err)
	}
	if err := storeTranscript(tx, parsed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	hits, err := SearchSessions(context.Background(), []session.SessionEntry{entry}, "after restart", 1, 80)
	if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "interrupted" || hits[0].Matches != 1 || !strings.Contains(hits[0].Snippets[0], "needle after restart") {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
}
