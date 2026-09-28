package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptySessionIDReportsUsageError(t *testing.T) {
	diagnostic, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	previous := os.Stderr
	os.Stderr = diagnostic
	t.Cleanup(func() { os.Stderr = previous })
	exit, tui := Run([]string{"show", "", "--json"})
	body, err := os.ReadFile(diagnostic.Name())
	if err != nil {
		t.Fatal(err)
	}
	var response Envelope
	if json.Unmarshal(body, &response) != nil || exit == 0 || tui || response.Error == nil || response.Error.Code != CodeUsage {
		t.Fatalf("invalid operand reported as application failure: %s", body)
	}
}

func TestSelectedProviderCommandsIgnoreBrokenOtherStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CODEX_SQLITE_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	for _, dir := range []string{"sessions", "claude"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "claude", "projects"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_meta","payload":{"id":"selected","cwd":"/work"}}
{"type":"event_msg","payload":{"type":"user_message","message":"evidence"}}
{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"file.txt":{"type":"add","content":"saved bytes"}}}}`
	if err := os.WriteFile(filepath.Join(root, "sessions", "rollout-selected.jsonl"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"show", "calls", "files", "cat", "diff", "resume"} {
		t.Run(command, func(t *testing.T) {
			result, err := execute(command, []string{"selected", "file.txt"}, Filter{Provider: "codex"}, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if command == "cat" && string(result.Raw) != "saved bytes" {
				t.Fatalf("recovered %q", result.Raw)
			}
		})
	}
}

func TestShowFullTextPreservesWhitespace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "codex"))
	dir := filepath.Join(root, "projects", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","message":{"role":"user","content":"first line\n  second line\n"}}`
	if err := os.WriteFile(filepath.Join(dir, "multiline.jsonl"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "first"} {
		result := runJSON(t, "show", "multiline", query, "--max-chars", "0")
		turn := result["data"].(map[string]any)["turns"].([]any)[0].(map[string]any)
		if turn["text"] != "first line\n  second line\n" {
			t.Fatalf("query %q changed recorded whitespace: %q", query, turn["text"])
		}
	}
}

func TestTextOutputReportsWriteFailure(t *testing.T) {
	out, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := Emit(out, FormatText, "schema", schemaResult()); err == nil {
		t.Fatal("failed stdout write reported success")
	}
}

func TestArgumentErrorHonorsParsedTextFormat(t *testing.T) {
	diagnostic, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	previous := os.Stderr
	os.Stderr = diagnostic
	t.Cleanup(func() { os.Stderr = previous })
	exit, tui := Run([]string{"show", "selected", "--output", "text", "--unknown"})
	content, err := os.ReadFile(diagnostic.Name())
	if err != nil {
		t.Fatal(err)
	}
	if exit == 0 || tui || !strings.HasPrefix(string(content), "error:") {
		t.Fatalf("exit=%d stderr=%s", exit, content)
	}
}
