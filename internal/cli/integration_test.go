package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
			`{"timestamp":"2026-09-26T08:00:02Z","type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"hello.txt":{"type":"add","content":"hello\n"}}}}`+"\n")
	claudePath := filepath.Join(root, "claude/projects/custom/claude-fixture.jsonl")
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(claudePath, old, old); err != nil {
		t.Fatal(err)
	}

	search := runJSON(t, "search", "portability needle", "--limit", "2")
	if got := search["data"].(map[string]any)["count"]; got != float64(2) {
		t.Fatalf("search count = %v, want both providers", got)
	}
	for _, flags := range [][]string{{"--claude", "--codex"}, {"--codex", "--claude"}} {
		result := runJSON(t, append([]string{"list"}, flags...)...)
		if result["data"].(map[string]any)["count"] != float64(2) {
			t.Errorf("combined provider flags did not select both stores: %v", result)
		}
	}
	for _, args := range [][]string{
		{"show", "--query", "portability", "--id", "codex-fixture"},
		{"show", "--id", "codex-fixture", "--query", "portability"},
		{"show", "--query", "portability", "codex-fixture"},
	} {
		result := runJSON(t, args...)
		turns := result["data"].(map[string]any)["turns"].([]any)
		if len(turns) != 1 || turns[0].(map[string]any)["text"] != "portability needle" {
			t.Fatalf("named operands selected the wrong conversation: %v", result)
		}
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
		result, err := execute("resume", []string{id}, Filter{}, 0, 0)
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

func TestSearchPreservesLiteralArguments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	projectDir := filepath.Join(root, "claude", "projects", "fixture")
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","sessionId":"literal-fixture","cwd":"/projects/--json","message":{"role":"user","content":"literal --json --query=needle --output=json"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "literal-fixture.jsonl"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		args  []string
		query string
	}{
		{"alias after terminator", []string{"--", "--json"}, "--json"},
		{"named operand after terminator", []string{"--", "--query=needle"}, "--query=needle"},
		{"multiple operands after terminator", []string{"--", "--json", "--query=needle"}, "--json --query=needle"},
		{"named query with equals", []string{"--query=--json"}, "--json"},
		{"named query with separate value", []string{"--query", "--json"}, "--json"},
		{"flag value resembling alias", []string{"--project", "--json", "literal"}, "literal"},
		{"terminator consumed as flag value", []string{"--project", "--", "literal", "--limit", "1"}, "literal"},
		{"alias value resembling alias", []string{"--workspace", "--json", "literal"}, "literal"},
		{"equals flag value", []string{"--project=--json", "literal"}, "literal"},
		{"single dash flag", []string{"-project", "--json", "literal"}, "literal"},
		{"trailing flags", []string{"literal", "--limit", "1"}, "literal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runJSON(t, append([]string{"search"}, tc.args...)...)
			data := result["data"].(map[string]any)
			if data["query"] != tc.query {
				t.Errorf("query = %q, want %q", data["query"], tc.query)
			}
			hits := data["hits"].([]any)
			if len(hits) != 1 || hits[0].(map[string]any)["id"] != "literal-fixture" {
				t.Errorf("search did not find the literal fixture: %v", hits)
			}
		})
	}
}

func TestCommandsReportTruncatedResults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	dir := filepath.Join(root, "claude", "projects", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first-session", "second-session"} {
		body := `{"type":"user","sessionId":"` + id + `","cwd":"/fixture","message":{"role":"user","content":"needle first turn"}}` + "\n" +
			`{"type":"assistant","message":{"role":"assistant","content":"needle second turn"}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		args       []string
		items      string
		knownTotal bool
	}{
		{[]string{"list"}, "sessions", true},
		{[]string{"search", "needle"}, "hits", true},
		{[]string{"show", "first-session"}, "turns", false},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			limited := runJSON(t, append(append([]string{}, tc.args...), "--limit", "1")...)
			data := limited["data"].(map[string]any)
			if got := len(data[tc.items].([]any)); got != 1 {
				t.Fatalf("limited result contains %d items, want 1", got)
			}
			trunc, ok := limited["truncated"].(map[string]any)
			if !ok || trunc["returned"] != float64(1) || trunc["has_more"] != true {
				t.Fatalf("partial result is not identified: %v", limited)
			}
			if tc.knownTotal && trunc["total"] != float64(2) {
				t.Errorf("total = %v, want 2", trunc["total"])
			}
			if !tc.knownTotal && trunc["total"] != nil {
				t.Errorf("lazy result invented a total: %v", trunc["total"])
			}
			complete := runJSON(t, append(append([]string{}, tc.args...), "--limit", "2")...)
			if len(complete["data"].(map[string]any)[tc.items].([]any)) != 2 || complete["truncated"] != nil {
				t.Fatalf("exact-fit result is not complete: %v", complete)
			}
		})
	}
}

func TestRecoveryReportsEarlierWriteAndEmitsItsBytes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	dir := filepath.Join(root, "claude", "projects", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","sessionId":"recovery-session","cwd":"/project","message":{"role":"user","content":"create a file"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"write","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"write"}]},"toolUseResult":{"type":"create","filePath":"/project/saved.txt","content":"saved\r\nbytes\n"}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"saved.txt":{"backupFileName":"expired@v2","version":2}}}}
`
	if err := os.WriteFile(filepath.Join(dir, "recovery-session.jsonl"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	listed := runJSON(t, "files", "recovery-session")
	files := listed["data"].(map[string]any)["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v, want one recoverable file", files)
	}
	file := files[0].(map[string]any)
	if file["path"] != "/project/saved.txt" || file["recoverable"] != true || file["recovery_source"] != "claude_write" || file["recovery_earlier_version"] != true {
		t.Fatalf("files did not disclose the available earlier Write: %v", file)
	}
	output := func(args ...string) []byte {
		t.Helper()
		out, err := os.CreateTemp(t.TempDir(), "stdout")
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		old := os.Stdout
		defer func() { os.Stdout = old }()
		os.Stdout = out
		exit, tui := Run(args)
		if exit != 0 || tui {
			t.Fatalf("%v: exit=%d interactive=%v", args, exit, tui)
		}
		got, err := os.ReadFile(out.Name())
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if text := string(output("files", "recovery-session", "--output", "text")); !strings.Contains(text, "earlier version") {
		t.Fatalf("human file listing hid the older revision: %q", text)
	}
	if got := output("cat", "recovery-session", "saved.txt"); string(got) != "saved\r\nbytes\n" {
		t.Fatalf("cat changed or wrapped the recovered bytes: %q", got)
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
	exit, tui := Run(append([]string{args[0], "--output", "json"}, args[1:]...))
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

func TestCLIHelpAndInvalidInput(t *testing.T) {
	for _, args := range [][]string{{"show", "--help"}, {"unknown-command"}, {"show", "unused", "--limit", "-1"}, {"show", "unused", "--limit", strconv.Itoa(math.MaxInt)}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			diag, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer diag.Close()
			oldOut, oldErr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = out, diag
			exit, tui := Run(args)
			os.Stdout, os.Stderr = oldOut, oldErr
			stdout, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			stderr, err := os.ReadFile(diag.Name())
			if err != nil {
				t.Fatal(err)
			}
			if tui {
				t.Fatal("unexpected interactive mode")
			}
			if len(args) > 1 && args[1] == "--help" {
				if exit != 0 || len(stderr) != 0 || !strings.Contains(string(stdout), "show") {
					t.Fatalf("help: exit %d stdout %q stderr %q", exit, stdout, stderr)
				}
			} else {
				var envelope Envelope
				if exit == 0 || len(stdout) != 0 || json.Unmarshal(stderr, &envelope) != nil || envelope.Error == nil || envelope.Error.Code != CodeUsage {
					t.Fatalf("invalid input: exit %d stdout %q stderr %q", exit, stdout, stderr)
				}
			}
		})
	}
}

func TestSubagentDisplayIDResolvesAndRejectsAmbiguity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	for _, parent := range []string{"parent-one", "parent-two"} {
		dir := filepath.Join(root, "claude", "projects", "project", parent, "subagents")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "agent-1234567890.jsonl")
		if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/project","message":{"role":"user","content":"subagent prompt"}}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if parent == "parent-one" {
			listed, err := List(Filter{Subagents: true})
			if err != nil {
				t.Fatal(err)
			}
			var display bytes.Buffer
			listed.Text(&display)
			fields := strings.Fields(display.String())
			if len(fields) == 0 {
				t.Fatal("list did not display the subagent")
			}
			result := runJSON(t, "show", fields[0])
			if result["data"].(map[string]any)["session"].(map[string]any)["id"] != parent+"/agent-1234567890" {
				t.Fatal("displayed id did not resolve")
			}
		}
	}
	if _, err := Resolve("agent-123456", ""); err == nil || classify(err) != CodeAmbiguous {
		t.Fatalf("ambiguous subagent id error = %v", err)
	}
}
