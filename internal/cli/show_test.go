package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowFindsLateEvidenceBeforeApplyingLimits(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	path := filepath.Join(root, "claude/projects/project/evidence.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","cwd":"/project","message":{"role":"user","content":"initial setup"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":"` + strings.Repeat("Ⱥİ ", 100) + `needle decision is experimental"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":"needle decision remains unapproved"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	preview := runJSON(t, "show", "evidence", "--limit", "1", "--max-chars", "4")
	first := preview["data"].(map[string]any)["turns"].([]any)[0].(map[string]any)
	if first["text_truncated"] != true {
		t.Fatalf("shortened turn was presented as complete: %v", first)
	}
	result := runJSON(t, "show", "evidence", "NEEDLE", "--limit", "1", "--max-chars", "60")
	turns := result["data"].(map[string]any)["turns"].([]any)
	if len(turns) != 1 {
		t.Fatalf("turns = %v", turns)
	}
	turn := turns[0].(map[string]any)
	if !strings.Contains(turn["text"].(string), "needle decision is experimental") || turn["line"] != float64(2) || turn["text_truncated"] != true {
		t.Fatalf("lost matching evidence or its source: %v", turn)
	}
	if trunc, ok := result["truncated"].(map[string]any); !ok || trunc["has_more"] != true {
		t.Fatalf("missing partial-result warning: %v", result)
	}
	complete := runJSON(t, "show", "evidence", "needle", "--max-chars", "0")
	full := complete["data"].(map[string]any)["turns"].([]any)
	if len(full) != 2 || !strings.HasPrefix(full[0].(map[string]any)["text"].(string), strings.Repeat("Ⱥİ ", 100)) || complete["truncated"] != nil {
		t.Fatalf("full matching evidence = %v", complete)
	}
}
