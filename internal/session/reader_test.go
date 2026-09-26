package session

import (
	"os"
	"path/filepath"
	"testing"
)

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
