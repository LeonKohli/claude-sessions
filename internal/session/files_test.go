package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

func TestRecoveryDecodesEscapedRecordTypes(t *testing.T) {
	for _, p := range []provider.Kind{provider.Claude, provider.Codex} {
		t.Run(p.String(), func(t *testing.T) {
			body := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"write","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"write"}]},"toolUseResult":{"type":"create","filePath":"/project/a.txt","content":"saved\n"}}`
			if p == provider.Codex {
				body = `{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/project/a.txt":{"type":"add","content":"saved\n"}}}}`
			}
			body = strings.NewReplacer("tool_use", `tool_\u0075se`, "tool_result", `tool_\u0072esult`, "patch_apply_end", `\u0070atch_apply_end`).Replace(body)
			e := SessionEntry{Provider: p, SessionID: "escaped", FullPath: writeRollout(t, body)}
			changes, err := FileChanges(e)
			if err != nil || len(changes) != 1 || changes[0].Path != "/project/a.txt" || !changes[0].Recoverable {
				t.Errorf("file evidence missing: %+v, %v", changes, err)
			}
			content, err := RecoverContent(e, "/project/a.txt")
			if err != nil || string(content) != "saved\n" {
				t.Errorf("recovered %q, %v", content, err)
			}
		})
	}
}

func TestClaudeRecoveryCannotLeaveCheckpointStore(t *testing.T) {
	for _, scenario := range []string{"backup traversal", "backup symlink", "session traversal", "session symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			dir := filepath.Join(root, "file-history", "safe")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(root, "outside")
			if err := os.WriteFile(outside, []byte("private bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			sessionID, backup := "safe", "../../outside"
			switch scenario {
			case "backup symlink":
				backup = "link"
				if err := os.Symlink(outside, filepath.Join(dir, backup)); err != nil {
					t.Fatal(err)
				}
			case "session traversal":
				sessionID, backup = "..", "outside"
			case "session symlink":
				sessionID, backup = "link", "outside"
				if err := os.Symlink(root, filepath.Join(root, "file-history", sessionID)); err != nil {
					t.Fatal(err)
				}
			}
			body := fmt.Sprintf(`{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a":{"backupFileName":%q,"version":1}}}}`, backup)
			e := SessionEntry{Provider: provider.Claude, SessionID: sessionID, FullPath: writeRollout(t, body)}
			if content, err := RecoverContent(e, "/project/a"); err == nil {
				t.Errorf("escaped store: %q", content)
			}
			changes, err := FileChanges(e)
			if err != nil || len(changes) != 1 || changes[0].Recoverable {
				t.Errorf("changes=%+v error=%v", changes, err)
			}
		})
	}
}

func TestClaudeRecoveryPreservesHistoryAfterRepeatedWrites(t *testing.T) {
	for _, firstKind := range []string{"create", "update"} {
		for _, checkpointExists := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/checkpoint=%t", firstKind, checkpointExists), func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("CLAUDE_CONFIG_DIR", root)
				if checkpointExists {
					dir := filepath.Join(root, "file-history", "writes")
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "hash@v2"), []byte("checkpoint"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				body := fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"first","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"first"}]},"toolUseResult":{"type":%q,"filePath":"/project/a.txt","content":"old content"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"second","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"second"}]},"toolUseResult":{"type":"update","filePath":"/project/a.txt","content":""}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a.txt":{"backupFileName":"hash@v2","version":2}}}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a.txt":{"backupFileName":"hash@v2","version":2}}}}
`, firstKind)
				e := SessionEntry{Provider: provider.Claude, SessionID: "writes", FullPath: writeRollout(t, body)}
				want, source, kind := "", "claude_write", ChangeUpdate
				if checkpointExists {
					want, source = "checkpoint", "claude_checkpoint"
				}
				if firstKind == "create" {
					kind = ChangeAdd
				}
				content, err := RecoverContent(e, "a.txt")
				if err != nil || content == nil || string(content) != want {
					t.Fatalf("content=%q error=%v", content, err)
				}
				changes, err := FileChanges(e)
				if err != nil || len(changes) != 1 {
					t.Fatalf("changes=%+v error=%v", changes, err)
				}
				c := changes[0]
				if c.Kind != kind || c.Revisions != 3 || !c.Recoverable || c.Bytes != len(want) || c.RecoverySource != source || c.RecoveryEarlierVersion != !checkpointExists {
					t.Errorf("history=%+v", c)
				}
			})
		}
	}
}

func TestClaudeRecoveryUsesConfirmedWriteResults(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	body := `{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"saved.txt":{"backupFileName":null,"version":1}}}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"write","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"write"}]},"toolUseResult":{"type":"create","filePath":"/project/saved.txt","content":"saved\r\nbytes\n"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"empty","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"empty"}]},"toolUseResult":{"type":"create","filePath":"/project/empty.txt","content":""}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"failed","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"failed","is_error":true}]},"toolUseResult":{"type":"update","filePath":"/project/saved.txt","content":"never written"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"shell","name":"Bash","input":{"command":"python generate.py"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"shell"}]},"toolUseResult":{"type":"create","filePath":"/project/guessed.txt","content":"untrusted shape"}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"missing"}]},"toolUseResult":{"type":"create","filePath":"/project/orphan.txt","content":"no call"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"multi-a","name":"Write"},{"type":"tool_use","id":"multi-b","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"multi-a"},{"type":"tool_result","tool_use_id":"multi-b"}]},"toolUseResult":{"type":"create","filePath":"/project/ambiguous.txt","content":"ambiguous result association"}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"saved.txt":{"backupFileName":null,"version":1}}}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"saved.txt":{"backupFileName":"expired@v2","version":2}}}}
`
	e := SessionEntry{Provider: provider.Claude, SessionID: "writes", ProjectPath: "/project", FullPath: writeRollout(t, body)}
	changes, err := FileChanges(e)
	if err != nil || len(changes) != 2 {
		t.Fatalf("files = %+v, error = %v", changes, err)
	}
	for _, c := range changes {
		if !c.Recoverable || c.Kind != ChangeAdd || c.RecoverySource != "claude_write" {
			t.Errorf("write metadata = %+v", c)
		}
		if c.RecoveryEarlierVersion != (c.Path == "/project/saved.txt") {
			t.Errorf("missing checkpoint must disclose earlier recovery: %+v", c)
		}
	}
	for path, want := range map[string]string{"saved.txt": "saved\r\nbytes\n", "empty.txt": ""} {
		got, err := RecoverContent(e, path)
		if err != nil || got == nil || string(got) != want {
			t.Errorf("recover %s = %q, error = %v", path, got, err)
		}
	}
}

func TestClaudeRecoveryCountsDistinctCheckpoints(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "file-history", "checkpoints")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"hash@v1": "first", "hash@v2": "second\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first := `{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a.txt":{"backupFileName":"hash@v1","version":1}}}}` + "\n"
	second := `{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a.txt":{"backupFileName":"hash@v2","version":2}}}}` + "\n"
	e := SessionEntry{Provider: provider.Claude, SessionID: "checkpoints", FullPath: writeRollout(t, first+first+second+second)}
	changes, err := FileChanges(e)
	if err != nil || len(changes) != 1 || changes[0].Revisions != 2 || !changes[0].Recoverable {
		t.Fatalf("checkpoints = %+v, error = %v", changes, err)
	}
	got, err := RecoverContent(e, "a.txt")
	if err != nil || string(got) != "second\n" {
		t.Fatalf("latest checkpoint = %q, error = %v", got, err)
	}
}

func TestClaudeRecoveryReturnsLatestConfirmedWrite(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	body := `{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a.txt":{"backupFileName":"expired@v1","version":1}}}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"first","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"first"}]},"toolUseResult":{"type":"update","filePath":"/project/a.txt","content":"first"}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"/project/a.txt":{"backupFileName":"expired@v1","version":1}}}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"second","name":"Write"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"second"}]},"toolUseResult":{"type":"update","filePath":"/project/a.txt","content":"second"}}
`
	e := SessionEntry{Provider: provider.Claude, FullPath: writeRollout(t, body)}
	got, err := RecoverContent(e, "a.txt")
	if err != nil || string(got) != "second" {
		t.Fatalf("latest Write = %q, error = %v", got, err)
	}
	changes, err := FileChanges(e)
	if err != nil || len(changes) != 1 || changes[0].Revisions != 3 || changes[0].RecoveryEarlierVersion {
		t.Fatalf("write history = %+v, error = %v", changes, err)
	}
}

func TestRecoveryUsesOnlySuccessfulPatches(t *testing.T) {
	body := `{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"saved.txt":{"type":"add","content":"actual content"},"empty.txt":{"type":"add","content":""}}}}
{"type":"event_msg","payload":{"type":"patch_apply_end","success":false,"changes":{"saved.txt":{"type":"add","content":"never written"},"rejected.txt":{"type":"add","content":"rejected"}}}}
`
	e := SessionEntry{Provider: provider.Codex, FullPath: writeRollout(t, body)}
	changes, err := FileChanges(e)
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes = %v, err %v", changes, err)
	}
	for _, c := range changes {
		if !c.Recoverable || c.Revisions != 1 {
			t.Errorf("invalid recovery metadata: %+v", c)
		}
	}
	for path, want := range map[string]string{"saved.txt": "actual content", "empty.txt": ""} {
		got, err := RecoverContent(e, path)
		if err != nil || got == nil || string(got) != want {
			t.Errorf("recover %s = %q, err %v; want %q", path, got, err, want)
		}
	}
	enriched, err := EnrichCodexSession(e.FullPath)
	if err != nil || len(enriched.FilesModified) != 2 {
		t.Errorf("enriched = %v, err %v", enriched, err)
	}
}

func TestRecoveryReadsCompletedFileChangeItems(t *testing.T) {
	body := `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","id":"call-1","status":"completed","changes":{"saved.txt":{"type":"add","content":"written"}}}}}
{"type":"event_msg","payload":{"type":"patch_apply_end","call_id":"call-1","success":true,"changes":{"saved.txt":{"type":"add","content":"written"}}}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","id":"call-2","status":"failed","changes":{"saved.txt":{"type":"add","content":"not written"}}}}}
`
	e := SessionEntry{Provider: provider.Codex, FullPath: writeRollout(t, body)}
	changes, err := FileChanges(e)
	if err != nil || len(changes) != 1 || changes[0].Revisions != 1 {
		t.Fatalf("changes=%v error=%v", changes, err)
	}
	content, err := RecoverContent(e, "saved.txt")
	if err != nil || string(content) != "written" {
		t.Fatalf("content=%q error=%v", content, err)
	}
}
