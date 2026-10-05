package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LeonKohli/claude-sessions/internal/util"
)

func TestSearchReportsBusyIndexInsteadOfWaitingIndefinitely(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
	dir, err := util.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "work.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(root, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	diag, err := os.CreateTemp(root, "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer diag.Close()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, diag
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	synctest.Test(t, func(t *testing.T) {
		// Release after the allowed wait so a broken timeout still completes.
		time.AfterFunc(6*time.Second, func() { lock.Close() })
		exit, interactive := Run([]string{"search", "needle", "--json"})
		stdout, err := os.ReadFile(out.Name())
		if err != nil {
			t.Fatal(err)
		}
		stderr, err := os.ReadFile(diag.Name())
		if err != nil {
			t.Fatal(err)
		}
		var response Envelope
		if exit == 0 || interactive || len(stdout) != 0 || json.Unmarshal(stderr, &response) != nil || response.Error == nil || response.Error.Code != CodeUnavailable {
			t.Fatalf("busy index: exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
		}
	})
}

func TestSearchReportsInvalidStoreInsteadOfNoMatches(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
			t.Setenv("CODEX_SQLITE_HOME", filepath.Join(root, "sqlite"))
			// Missing providers are allowed; this also warms the discovery cache.
			runJSON(t, "search", "needle", "--agent", agent)
			store := filepath.Join(root, agent, "projects")
			if agent == "codex" {
				store = filepath.Join(root, agent, "sessions")
			}
			if err := os.MkdirAll(filepath.Dir(store), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := os.CreateTemp(root, "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			diag, err := os.CreateTemp(root, "stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer diag.Close()
			oldOut, oldErr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = out, diag
			exit, interactive := Run([]string{"search", "needle", "--agent", agent, "--json"})
			os.Stdout, os.Stderr = oldOut, oldErr
			stdout, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			stderr, err := os.ReadFile(diag.Name())
			if err != nil {
				t.Fatal(err)
			}
			var response Envelope
			if exit == 0 || interactive || len(stdout) != 0 || json.Unmarshal(stderr, &response) != nil || response.Error == nil || response.Error.Code != CodeUnavailable {
				t.Fatalf("invalid store: exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
			}
			other := "codex"
			if agent == "codex" {
				other = "claude"
			}
			runJSON(t, "search", "needle", "--agent", other)
		})
	}
}
