package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallsFindToolInputsBeyondConversationAndOutputLimits(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
			t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
			path := filepath.Join(root, "claude/projects/project/calls-fixture.jsonl")
			body := `{"type":"user","cwd":"/project","message":{"role":"user","content":"inspect old commands"}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"first","name":"Bash","input":{"command":"unrelated"}},{"type":"tool_use","id":"target","name":"Bash","input":{"command":"` + strings.Repeat("prefix ", 100) + `snort --pcap-dir /captures"}}]}}` + "\n" +
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"target","is_error":true,"content":"output must not become a call --pcap-dir"}]}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"later","name":"Bash","input":{"command":"snort --pcap-dir /other"}}]}}` + "\n"
			tool := "Bash"
			if agent == "codex" {
				path = filepath.Join(root, "codex/sessions/calls-fixture.jsonl")
				tool = "exec_command"
				body = `{"type":"session_meta","payload":{"id":"calls-fixture","cwd":"/project"}}` + "\n" +
					`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect old commands"}]}}` + "\n" +
					`{"type":"response_item","payload":{"type":"function_call","call_id":"target","name":"exec_command","arguments":"` + strings.Repeat("prefix ", 100) + `snort --pcap-dir /captures"}}` + "\n" +
					`{"type":"response_item","payload":{"type":"function_call_output","call_id":"target","output":"output must not become a call --pcap-dir"}}` + "\n" +
					`{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"later","name":"exec_command","input":"snort --pcap-dir /other"}}` + "\n"
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			out := runJSON(t, "calls", "calls-fixture", "PCAP-DIR", "--limit", "1", "--max-chars", "50")
			calls := out["data"].(map[string]any)["calls"].([]any)
			if len(calls) != 1 {
				t.Fatalf("calls = %v", calls)
			}
			call := calls[0].(map[string]any)
			input := call["input"].(string)
			if call["id"] != "target" || call["tool"] != tool || !strings.Contains(input, "--pcap-dir /captures") || len([]rune(input)) > 50 || call["input_truncated"] != true || call["line"].(float64) < 2 {
				t.Fatalf("wrong or unusable call: %v", call)
			}
			if out["truncated"].(map[string]any)["has_more"] != true {
				t.Fatalf("more matching calls not disclosed: %v", out)
			}
			full := runJSON(t, "calls", "calls-fixture", "target", "--max-chars", "0")
			fullCalls := full["data"].(map[string]any)["calls"].([]any)
			if len(fullCalls) != 1 || !strings.Contains(fullCalls[0].(map[string]any)["input"].(string), strings.Repeat("prefix ", 100)) || fullCalls[0].(map[string]any)["input_truncated"] == true {
				t.Fatalf("cannot retrieve the full input by call id: %v", full)
			}
		})
	}
}
