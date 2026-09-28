package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

func TestCodexHistoryIncludesBoundedAncestors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	dir := filepath.Join(root, "archived_sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const parentID = "01a0dcdc-e347-7421-8a61-4ee973b5e1a3"
	parent := filepath.Join(dir, "rollout-2026-09-26T00-00-00-"+parentID+".jsonl")
	prefix := `{"ordinal":0,"type":"session_meta","payload":{"id":"another-thread","history_mode":"paginated"}}` + "\n" +
		`{"ordinal":1,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"inherited question"}]}}` + "\n"
	suffix := `{"ordinal":2,"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"discarded answer"}]}}` + "\n"
	if err := os.WriteFile(parent, []byte(prefix+suffix), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		bytes     int
		id        string
		wantError bool
	}{
		{"bounded history", len(prefix), parentID, false},
		{"ordinal also bounds history", len(prefix + suffix), parentID, false},
		{"missing ancestor", len(prefix), "missing", true},
		{"short ancestor", len(prefix+suffix) + 1, parentID, true},
		{"partial record", len(prefix) - 2, parentID, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := writeRollout(t, fmt.Sprintf(`{"ordinal":2,"type":"session_meta","payload":{"id":"child","history_mode":"paginated","history_base":{"thread_id":%q,"end_ordinal_exclusive":2,"end_byte_offset":%d}}}`+"\n"+
				`{"ordinal":3,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"new direction"}]}}`+"\n", tc.id, tc.bytes))
			var texts []string
			err := WalkCodexText(context.Background(), child, func(line SearchableLine) bool {
				texts = append(texts, line.Text)
				return true
			})
			if tc.wantError {
				if err == nil {
					t.Fatalf("broken history returned success: %v", texts)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(texts, []string{"inherited question", "new direction"}) {
				t.Fatalf("history = %v", texts)
			}
		})
	}
}

func TestCodexCompressedHistoryFailsExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl.zst")
	if err := os.WriteFile(path, []byte{0x28, 0xb5, 0x2f, 0xfd}, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := collectCodexText(path)
	if !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("compressed history returned %v", err)
	}
}

func TestCodexInheritedToolAndFileEvidence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const parentID = "01a0dcdc-e347-7421-8a61-4ee973b5e1a3"
	parent := filepath.Join(dir, "rollout-2026-09-26T00-00-00-"+parentID+".jsonl")
	prefix := `{"ordinal":0,"type":"session_meta","payload":{"id":"parent","history_mode":"paginated"}}` + "\n" +
		`{"ordinal":1,"type":"response_item","payload":{"type":"function_call","call_id":"write","name":"apply_patch","arguments":"inherited input"}}` + "\n" +
		`{"ordinal":2,"type":"event_msg","payload":{"type":"patch_apply_end","call_id":"write","success":true,"changes":{"saved.txt":{"type":"add","content":"saved\r\nbytes\n"}}}}` + "\n"
	suffix := `{"ordinal":3,"type":"event_msg","payload":{"type":"patch_apply_end","call_id":"discarded","success":true,"changes":{"saved.txt":{"type":"add","content":"wrong branch"}}}}` + "\n"
	if err := os.WriteFile(parent, []byte(prefix+suffix), 0600); err != nil {
		t.Fatal(err)
	}
	child := writeRollout(t, fmt.Sprintf(`{"ordinal":3,"type":"session_meta","payload":{"id":"child","history_mode":"paginated","history_base":{"thread_id":%q,"end_ordinal_exclusive":3,"end_byte_offset":%d}}}`+"\n", parentID, len(prefix)))
	t.Run("calls", func(t *testing.T) {
		var calls []ToolCall
		err := WalkToolCalls(provider.Codex, child, func(call ToolCall) bool { calls = append(calls, call); return true })
		if err != nil || len(calls) != 1 || calls[0].Input != "inherited input" || calls[0].Line != 2 {
			t.Fatalf("calls = %+v, error = %v", calls, err)
		}
	})
	t.Run("recovery", func(t *testing.T) {
		got, err := RecoverContent(SessionEntry{Provider: provider.Codex, FullPath: child}, "saved.txt")
		if err != nil || string(got) != "saved\r\nbytes\n" {
			t.Fatalf("recovered %q, error = %v", got, err)
		}
	})
}
