package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

func TestConversationReadersPreserveTextAcrossJSONEncodings(t *testing.T) {
	for _, p := range []provider.Kind{provider.Claude, provider.Codex} {
		for _, encoding := range []string{"compact", "spaced", "escaped"} {
			t.Run(p.String()+"/"+encoding, func(t *testing.T) {
				body := `{"type":"assistant","message":{"role":"assistant","content":"  answer\n    code\n"}}`
				if p == provider.Codex {
					body = `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"  answer\n    code\n"}]}}`
				}
				if encoding == "spaced" {
					body = strings.ReplaceAll(body, ":", " :\t ")
				} else if encoding == "escaped" {
					body = strings.NewReplacer("type", `ty\u0070e`, "assistant", `\u0061ssistant`, "response_item", `\u0072esponse_item`).Replace(body)
				}
				path := filepath.Join(t.TempDir(), "session.jsonl")
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				want := "  answer\n    code\n"
				preview, err := ReadPreview(p, path, 1)
				if err != nil || len(preview) != 1 || preview[0].Text != want {
					t.Errorf("preview lost text: %+v, %v", preview, err)
				}
				var turns []SearchableLine
				err = WalkSearchable(context.Background(), p, path, func(line SearchableLine) bool {
					turns = append(turns, line)
					return true
				})
				if err != nil || len(turns) != 1 || turns[0].Text != want || turns[0].LineNum != 1 {
					t.Errorf("search lost text or provenance: %+v, %v", turns, err)
				}
				usage, err := Enrich(p, path)
				if err != nil || usage.MessageCount != 1 {
					t.Errorf("usage disagrees with conversation: %+v, %v", usage, err)
				}
			})
		}
	}
}

func TestClaudeConversationExcludesNonMessageRecords(t *testing.T) {
	body := `{"type":"progress","data":{"type":"user"},"message":{"role":"user","content":"not a conversation turn"}}
{"type":"assistant","message":{"role":"assistant","content":"recorded answer"}}
`
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var turns []SearchableLine
	err := WalkSearchable(context.Background(), provider.Claude, path, func(line SearchableLine) bool {
		turns = append(turns, line)
		return true
	})
	if err != nil || len(turns) != 1 || turns[0].Text != "recorded answer" || turns[0].LineNum != 2 {
		t.Fatalf("non-message record leaked into conversation: %+v, %v", turns, err)
	}
}

// Claude transcripts open with bookkeeping records whose cwd is null. Reading
// the working directory from line 0 alone leaves it empty, which forces the
// caller to guess the project from its ambiguously encoded directory name —
// and that guess turns every hyphen into a slash, so `resume` hands back a
// `cd` into a directory that does not exist.
func TestReadFirstUserPromptFindsCWDPastNullPreamble(t *testing.T) {
	body := `{"type":"last-prompt","cwd":null,"timestamp":"2026-07-27T10:00:00Z"}
{"type":"mode","cwd":null}
{"type":"permission-mode","cwd":null}
{"type":"attachment","cwd":"/Users/example/projects/client-app/ui-components"}
{"type":"file-history-snapshot","cwd":null}
{"type":"user","cwd":"/Users/example/projects/client-app/ui-components","gitBranch":"main","timestamp":"2026-07-27T10:00:05Z","message":{"role":"user","content":"the first prompt"}}
`
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt, cwd, branch, ts := ReadFirstUserPrompt(path)

	want := "/Users/example/projects/client-app/ui-components"
	if cwd != want {
		t.Errorf("cwd = %q, want %q", cwd, want)
	}
	if prompt != "the first prompt" {
		t.Errorf("prompt = %q", prompt)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
	if ts.IsZero() {
		t.Error("timestamp not captured")
	}
}

func TestReadersContinuePastLargeToolOutput(t *testing.T) {
	for _, codex := range []bool{false, true} {
		body := `{"type":"tool_output","content":"` + strings.Repeat("x", 9<<20) + "\"}\n"
		if codex {
			body += `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"after large output"}]}}`
		} else {
			body += `{"type":"assistant","message":{"role":"assistant","content":"after large output"}}`
		}
		path := filepath.Join(t.TempDir(), "large.jsonl")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var msgs []PreviewMessage
		var err error
		if codex {
			msgs, err = ReadPreview(provider.Codex, path, 10)
		} else {
			msgs, err = ReadPreview(provider.Claude, path, 10)
		}
		if err != nil || len(msgs) != 1 || msgs[0].Text != "after large output" {
			t.Errorf("codex=%v: messages=%v err=%v", codex, msgs, err)
		}
	}
}

// A transcript that never records a cwd must not invent one.
func TestReadFirstUserPromptWithoutCWD(t *testing.T) {
	body := `{"type":"last-prompt","cwd":null}
{"type":"user","message":{"role":"user","content":"hello"}}
`
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt, cwd, _, _ := ReadFirstUserPrompt(path)
	if cwd != "" {
		t.Errorf("cwd = %q, want empty", cwd)
	}
	if prompt != "hello" {
		t.Errorf("prompt = %q", prompt)
	}
}

// The cwd on the opening line is still preferred when present.
func TestReadFirstUserPromptPrefersEarliestCWD(t *testing.T) {
	body := `{"type":"summary","cwd":"/first/path","timestamp":"2026-07-27T10:00:00Z"}
{"type":"user","cwd":"/later/path","message":{"role":"user","content":"hi"}}
`
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, cwd, _, _ := ReadFirstUserPrompt(path); cwd != "/first/path" {
		t.Errorf("cwd = %q, want /first/path", cwd)
	}
}

// Search skips tool records without decoding them, so their contents must not
// end a record early, leak in as conversation, or hide neighbouring text.
func TestSearchableTextIgnoresToolRecordContents(t *testing.T) {
	tricky := `quote \" backslash \\\\ \"type\":\"text\",\"text\":\"forged\" } ] {`
	cases := []struct {
		name string
		kind provider.Kind
		body string
		want []string
	}{
		{"claude text beside tool result", provider.Claude,
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"` + tricky + `"},{"type":"text","text":"kept"}]}}` + "\n",
			[]string{"kept"}},
		{"claude tool result only", provider.Claude,
			`{"message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"nested output"}]}]},"type":"user","toolUseResult":"` + tricky + `"}` + "\n" +
				`{"type":"assistant","message":{"role":"assistant","content":"answer"}}` + "\n",
			[]string{"answer"}},
		{"claude truncated final record", provider.Claude,
			`{"type":"assistant","message":{"role":"assistant","content":"complete"}}` + "\n" +
				`{"type":"assistant","message":{"role":"assistant","content":"partial`,
			[]string{"complete"}},
		{"codex tool output resembling a message", provider.Codex,
			`{"type":"response_item","payload":{"type":"function_call_output","output":"{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"forged\"}]} ` + tricky + `"}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}` + "\n",
			[]string{"answer"}},
		{"codex truncated final record", provider.Codex,
			`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"complete"}]}}` + "\n" +
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial`,
			[]string{"complete"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(c.body), 0600); err != nil {
				t.Fatal(err)
			}
			var got []string
			err := WalkSearchable(context.Background(), c.kind, path, func(line SearchableLine) bool {
				got = append(got, line.Text)
				return true
			})
			if err != nil || strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("searchable text = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}
