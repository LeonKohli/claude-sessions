package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexStateDBUsesSQLiteHome(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("CODEX_SQLITE_HOME", dir)
	for _, name := range []string{"state_2.sqlite", "state_10.sqlite"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := CodexStateDB(), filepath.Join(dir, "state_10.sqlite"); got != want {
		t.Fatalf("state DB = %q, want %q", got, want)
	}
}
