package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchHitOpensItsPhysicalRollout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "codex"))
	dir := filepath.Join(root, "codex", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const thread = "01a0dac5-f897-7121-b3ef-5e0ccc0c75ea"
	for i, rollout := range []string{thread, "01a0dac8-0d70-78e3-baf1-01b02d9bd319", "01a0dcdc-e347-7421-8a61-4ee973b5e1a3"} {
		text := []string{"original evidence", "older evidence", "newer evidence"}[i]
		path := filepath.Join(dir, "rollout-2026-09-26T00-00-00-"+thread+"_"+rollout+".jsonl")
		if rollout == thread {
			path = filepath.Join(dir, "rollout-2026-09-26T00-00-00-"+thread+".jsonl")
		}
		body := `{"type":"session_meta","payload":{"id":"` + thread + `","cwd":"/tmp"}}` + "\n" +
			`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		at := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"original evidence", "older evidence", "newer evidence"} {
		hits := runJSON(t, "search", want)["data"].(map[string]any)["hits"].([]any)
		if len(hits) != 1 {
			t.Fatalf("search %q = %v", want, hits)
		}
		id := hits[0].(map[string]any)["id"].(string)
		data := runJSON(t, "show", id)["data"].(map[string]any)
		turns := data["turns"].([]any)
		if len(turns) != 1 || turns[0].(map[string]any)["text"] != want {
			t.Fatalf("search hit %q opened the wrong history: %v", want, turns)
		}
		resume := runJSON(t, "resume", id)["data"].(map[string]any)
		if resume["command"] != "codex resume '"+thread+"'" {
			t.Fatalf("resume must use logical thread identity: %v", resume)
		}
	}
	if _, err := Resolve(thread, ""); err == nil || !strings.Contains(err.Error(), "matches 3") {
		t.Fatalf("ambiguous logical identity silently selected a rollout: %v", err)
	}
}
