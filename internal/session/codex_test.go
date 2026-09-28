package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

// A rollout that exercises the shapes this package cares about: the leading
// session_meta, a turn_context carrying the model, both conversation event
// kinds, a running token total, a tool call, and an applied patch.
const sampleRollout = `{"timestamp":"2026-07-27T15:15:47.186Z","type":"session_meta","payload":{"session_id":"019f-parent","id":"019f-self","cwd":"/tmp/work","originator":"codex-tui","cli_version":"0.145.0","thread_source":"user","source":"cli","git":{"branch":"main","commit_hash":"abc"}}}
{"timestamp":"2026-07-27T15:15:48.000Z","type":"turn_context","payload":{"turn_id":"t1","cwd":"/tmp/work","model":"gpt-5.6-sol","effort":"high"}}
{"timestamp":"2026-07-27T15:15:49.000Z","type":"event_msg","payload":{"type":"task_started"}}
{"timestamp":"2026-07-27T15:15:50.000Z","type":"event_msg","payload":{"type":"user_message","message":"first prompt here","images":[]}}
{"timestamp":"2026-07-27T15:15:51.000Z","type":"event_msg","payload":{"type":"agent_reasoning","text":"thinking"}}
{"timestamp":"2026-07-27T15:15:52.000Z","type":"event_msg","payload":{"type":"agent_message","message":"the reply","phase":"final"}}
{"timestamp":"2026-07-27T15:15:53.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{}"}}
{"timestamp":"2026-07-27T15:15:54.000Z","type":"event_msg","payload":{"type":"patch_apply_end","call_id":"c1","success":true,"changes":{"/tmp/work/a.go":{"type":"add"},"/tmp/work/b.go":{"type":"update"}}}}
{"timestamp":"2026-07-27T15:15:55.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":5,"output_tokens":20,"total_tokens":120}}}}
{"timestamp":"2026-07-27T15:15:56.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":180,"cache_write_input_tokens":9,"output_tokens":50,"total_tokens":350}}}}
`

func writeRollout(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadCodexHead(t *testing.T) {
	meta, prompt, model, ts, ok := ReadCodexHead(writeRollout(t, sampleRollout))
	if !ok {
		t.Fatal("expected session_meta to parse")
	}
	// The thread's own id, not the parent it was spawned from, is what
	// `codex resume` takes.
	if meta.ID != "019f-self" {
		t.Errorf("ID = %q, want 019f-self", meta.ID)
	}
	if meta.CWD != "/tmp/work" {
		t.Errorf("CWD = %q", meta.CWD)
	}
	if meta.Git == nil || meta.Git.Branch != "main" {
		t.Errorf("git branch not parsed: %+v", meta.Git)
	}
	if prompt != "first prompt here" {
		t.Errorf("prompt = %q", prompt)
	}
	if model != "gpt-5.6-sol" {
		t.Errorf("model = %q", model)
	}
	if ts.IsZero() {
		t.Error("timestamp not parsed")
	}
	if meta.IsSubagent() {
		t.Error("thread_source=user must not read as a subagent")
	}
}

// Threads spawned before thread_source existed only record their parentage
// inside the free-form source object.
func TestCodexSubagentDetection(t *testing.T) {
	cases := []struct {
		name string
		meta CodexMeta
		want bool
	}{
		{"explicit", CodexMeta{ThreadSource: "subagent"}, true},
		{"legacy source object", CodexMeta{Source: []byte(`{"subagent":{"other":"guardian"}}`)}, true},
		{"user thread", CodexMeta{ThreadSource: "user", Source: []byte(`"cli"`)}, false},
	}
	for _, c := range cases {
		if got := c.meta.IsSubagent(); got != c.want {
			t.Errorf("%s: IsSubagent() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCodexAgentLabel(t *testing.T) {
	cases := []struct {
		meta CodexMeta
		want string
	}{
		{CodexMeta{Nickname: "Wegener", Role: "explorer"}, "Wegener/explorer"},
		{CodexMeta{Nickname: "Wegener"}, "Wegener"},
		{CodexMeta{Role: "review"}, "review"},
		{CodexMeta{}, ""},
	}
	for _, c := range cases {
		if got := c.meta.AgentLabel(); got != c.want {
			t.Errorf("AgentLabel(%+v) = %q, want %q", c.meta, got, c.want)
		}
	}
}

// A resumed thread replays its prior history before the user's first turn, so
// the header scan must not give up after a fixed number of lines.
func TestReadCodexHeadPastDeepPreamble(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"timestamp":"2026-07-27T15:15:47.186Z","type":"session_meta","payload":{"id":"deep","cwd":"/tmp/deep"}}` + "\n")
	for i := 0; i < 5000; i++ {
		b.WriteString(`{"timestamp":"2026-07-27T15:15:48.000Z","type":"response_item","payload":{"type":"reasoning","text":"filler"}}` + "\n")
	}
	b.WriteString(`{"timestamp":"2026-07-27T15:20:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"buried prompt"}}` + "\n")

	meta, prompt, _, _, ok := ReadCodexHead(writeRollout(t, b.String()))
	if !ok || meta.ID != "deep" {
		t.Fatalf("meta not parsed: ok=%v id=%q", ok, meta.ID)
	}
	if prompt != "buried prompt" {
		t.Errorf("prompt = %q, want the message at line 5002", prompt)
	}
}

func TestReadCodexMessages(t *testing.T) {
	msgs, err := ReadPreview(provider.Codex, writeRollout(t, sampleRollout), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (reasoning and task events are not dialogue)", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Text != "first prompt here" {
		t.Errorf("first message = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Text != "the reply" {
		t.Errorf("second message = %+v", msgs[1])
	}
	if msgs[0].Timestamp.IsZero() {
		t.Error("message timestamp not parsed")
	}
}

func TestEnrichCodexSession(t *testing.T) {
	data, err := EnrichCodexSession(writeRollout(t, sampleRollout))
	if err != nil {
		t.Fatal(err)
	}
	// Codex reports running totals, so the last token_count wins outright
	// rather than being summed with earlier ones.
	if data.TotalInputTokens != 300 || data.TotalOutputTokens != 50 {
		t.Errorf("tokens = in:%d out:%d, want in:300 out:50 (last total, not a sum)",
			data.TotalInputTokens, data.TotalOutputTokens)
	}
	if data.CacheReadTokens != 180 || data.CacheWriteTokens != 9 {
		t.Errorf("cache = read:%d write:%d", data.CacheReadTokens, data.CacheWriteTokens)
	}
	if data.Model != "gpt-5.6-sol" {
		t.Errorf("model = %q", data.Model)
	}
	if data.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", data.MessageCount)
	}
	if len(data.ToolsUsed) != 1 || data.ToolsUsed[0] != "exec_command" {
		t.Errorf("ToolsUsed = %v", data.ToolsUsed)
	}
	if len(data.FilesModified) != 2 {
		t.Errorf("FilesModified = %v, want both patched files", data.FilesModified)
	}
}

func TestCodexConversationText(t *testing.T) {
	lines, err := collectCodexText(writeRollout(t, sampleRollout))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d searchable lines, want 2", len(lines))
	}
	if lines[0].LineNum != 4 {
		t.Errorf("LineNum = %d, want the actual file line 4", lines[0].LineNum)
	}
}

// Truncated or partially written rollouts must not take the scan down.
func TestCodexReadersTolerateMalformedLines(t *testing.T) {
	body := sampleRollout + "{not json\n" + `{"type":"event_msg"}` + "\n"
	path := writeRollout(t, body)

	if _, _, _, _, ok := ReadCodexHead(path); !ok {
		t.Error("ReadCodexHead failed on trailing garbage")
	}
	if msgs, err := ReadPreview(provider.Codex, path, 30); err != nil || len(msgs) != 2 {
		t.Errorf("ReadCodexMessages: %d msgs, err %v", len(msgs), err)
	}
	if _, err := EnrichCodexSession(path); err != nil {
		t.Errorf("EnrichCodexSession: %v", err)
	}
}

func TestShortenModelLeavesCodexIDsAlone(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-6-20260115": "opus-4.6",
		"claude-sonnet-4-5":        "sonnet-4.5",
		"gpt-5.6-sol":              "gpt-5.6-sol",
		"codex-auto-review":        "codex-auto-review",
		"":                         "",
	}
	for in, want := range cases {
		if got := ShortenModel(in); got != want {
			t.Errorf("ShortenModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodexResponseMessages(t *testing.T) {
	response := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"modern prompt"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"modern reply"}]}}
`
	legacy := `{"type":"event_msg","payload":{"type":"user_message","message":"modern prompt"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"modern reply"}}
`
	for _, body := range []string{response, legacy + response} {
		path := writeRollout(t, body)
		msgs, err := ReadPreview(provider.Codex, path, 10)
		if err != nil || len(msgs) != 2 || msgs[0].Text != "modern prompt" || msgs[1].Text != "modern reply" {
			t.Fatalf("response conversation = %v, err %v", msgs, err)
		}
		lines, err := collectCodexText(path)
		if err != nil || len(lines) != 2 || lines[1].Text != "modern reply" {
			t.Fatalf("searchable conversation = %v, err %v", lines, err)
		}
	}
}

func BenchmarkReadCodexMessages(b *testing.B) {
	path := filepath.Join(b.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(sampleRollout), 0600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ReadPreview(provider.Codex, path, 30); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCodexLimitedReadPrefersResponseAfterLegacyEvents(t *testing.T) {
	path := writeRollout(t, `{"type":"event_msg","payload":{"type":"user_message","message":"legacy duplicate"}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"canonical response"}]}}
{"type":"event_msg","payload":{"type":"agent_message","message":"later duplicate"}}
`)
	messages, err := ReadPreview(provider.Codex, path, 1)
	if err != nil || len(messages) != 1 || messages[0].Role != "assistant" || messages[0].Text != "canonical response" {
		t.Fatalf("limited conversation = %+v, error = %v", messages, err)
	}
}

func collectCodexText(path string) ([]SearchableLine, error) {
	var lines []SearchableLine
	err := WalkCodexText(context.Background(), path, func(line SearchableLine) bool {
		lines = append(lines, line)
		return true
	})
	return lines, err
}
