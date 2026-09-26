package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandsWithRelocatedStores(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	project := filepath.Join(root, "project with spaces and 'quotes'")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	quotedProject, _ := json.Marshal(project)
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "claude/projects/custom/claude-fixture.jsonl"),
		`{"type":"user","sessionId":"claude-fixture","cwd":`+string(quotedProject)+`,"timestamp":"2026-09-26T08:00:00Z","message":{"role":"user","content":"portability needle"}}`+"\n"+
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"fixture reply"}]}}`+"\n")
	write(filepath.Join(root, "codex/sessions/2026/09/26/rollout-2026-09-26T08-00-00-codex-fixture.jsonl"),
		`{"timestamp":"2026-09-26T08:00:00Z","type":"session_meta","payload":{"id":"codex-fixture","cwd":`+string(quotedProject)+`}}`+"\n"+
			`{"timestamp":"2026-09-26T08:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"portability needle"}}`+"\n"+
			`{"timestamp":"2026-09-26T08:00:02Z","type":"event_msg","payload":{"type":"patch_apply_end","changes":{"hello.txt":{"type":"add","content":"hello\n"}}}}`+"\n")
	claudePath := filepath.Join(root, "claude/projects/custom/claude-fixture.jsonl")
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(claudePath, old, old); err != nil {
		t.Fatal(err)
	}

	search := runJSON(t, "search", "portability needle", "--limit", "2")
	if got := search["data"].(map[string]any)["count"]; got != float64(2) {
		t.Fatalf("search count = %v, want both providers", got)
	}
	for _, id := range []string{"claude-fixture", "codex-fixture"} {
		show := runJSON(t, "show", id)
		if len(show["data"].(map[string]any)["turns"].([]any)) == 0 {
			t.Errorf("show %s has no turns", id)
		}
		files := runJSON(t, "files", id)
		want := float64(0)
		if id == "codex-fixture" {
			want = 1
		}
		if got := files["data"].(map[string]any)["count"]; got != want {
			t.Errorf("files %s count = %v, want %v", id, got, want)
		}
		resume := runJSON(t, "resume", id)["data"].(map[string]any)
		if resume["cwd"] != project || resume["cwd_exists"] != true {
			t.Errorf("resume %s lost the project: %v", id, resume)
		}
		result, err := Resume(id)
		if err != nil {
			t.Fatal(err)
		}
		var text bytes.Buffer
		result.Text(&text)
		if !strings.HasPrefix(text.String(), "cd '"+strings.ReplaceAll(project, "'", "'\\''")+"' && ") {
			t.Errorf("resume %s did not quote its working directory: %s", id, text.String())
		}
	}
	f, err := os.OpenFile(claudePath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"user","message":{"role":"user","content":"newly appended needle"}}` + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	show := runJSON(t, "list")["data"].(map[string]any)
	if got := show["sessions"].([]any)[0].(map[string]any)["id"]; got != "claude-fixture" {
		t.Errorf("list newest session after append = %v, want claude-fixture", got)
	}
}

func runJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	exit, tui := Run(append(args, "--output", "json"))
	os.Stdout = old
	w.Close()
	body, err := io.ReadAll(r)
	r.Close()
	if err != nil || exit != 0 || tui {
		t.Fatalf("%v: exit=%d tui=%v err=%v output=%s", args, exit, tui, err, body)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true {
		t.Fatalf("%v failed: %s", args, body)
	}
	return envelope
}
