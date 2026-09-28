package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeUsageCountsResponsesNotContentFragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	body := `{"type":"user","message":{"role":"user","content":"question"}}
{"type":"assistant","message":{"id":"response-one","role":"assistant","usage":{"input_tokens":3,"output_tokens":1,"cache_creation_input_tokens":10174},"content":[{"type":"thinking","thinking":"reasoning"}]}}
{"type":"assistant","message":{"id":"response-one","role":"assistant","usage":{"input_tokens":3,"output_tokens":11,"cache_creation_input_tokens":10174},"content":[{"type":"text","text":"answer"}]}}
{"type":"assistant","message":{"id":"response-one","role":"assistant","usage":{"input_tokens":3,"output_tokens":11,"cache_creation_input_tokens":10174},"content":[{"type":"tool_use","name":"Read"}]}}
{"type":"assistant","message":{"id":"response-two","role":"assistant","usage":{"input_tokens":7,"output_tokens":2},"content":[{"type":"text","text":"next answer"}]}}
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := EnrichClaudeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalInputTokens != 10 || got.TotalOutputTokens != 13 || got.CacheWriteTokens != 10174 || got.MessageCount != 3 {
		t.Fatalf("usage counted fragments or missed the final usage: %+v", got)
	}
	if len(got.ToolsUsed) != 1 || got.ToolsUsed[0] != "Read" {
		t.Fatalf("lost tool from another fragment: %v", got.ToolsUsed)
	}
}

// Claude records tracked files relative to the project root and keeps the real
// directory alongside. Codex always reports absolute paths, so Claude's must be
// resolved or the two providers disagree about what a file path means.
func TestFileBackupAbsPath(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		backup fileBackup
		want   string
	}{
		{
			name:   "relative key resolved against realParentDir",
			key:    "app/globals.css",
			backup: fileBackup{RealParentDir: "/Users/example/project/app", BackupFileName: "90c2@v2", Version: 2},
			want:   "/Users/example/project/app/globals.css",
		},
		{
			name:   "absolute key left alone",
			key:    "/tmp/scratch/probe.ts",
			backup: fileBackup{RealParentDir: "/tmp/scratch"},
			want:   "/tmp/scratch/probe.ts",
		},
		{
			name:   "missing realParentDir falls back to the key",
			key:    ".env",
			backup: fileBackup{},
			want:   ".env",
		},
		{
			name:   "bare key with a parent dir",
			key:    ".env",
			backup: fileBackup{RealParentDir: "/Users/example/project"},
			want:   "/Users/example/project/.env",
		},
	}
	for _, c := range cases {
		if got := c.backup.absPath(c.key); got != c.want {
			t.Errorf("%s: absPath(%q) = %q, want %q", c.name, c.key, got, c.want)
		}
	}
}
