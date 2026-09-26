package session

import "testing"

// Claude records tracked files relative to the project root and keeps the real
// directory alongside. Codex always reports absolute paths, so Claude's must be
// resolved or the two providers disagree about what a file path means.
func TestFileBackupAbsPath(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		backup fileBackup
		want   string
	}{
		{
			name:   "relative key resolved against realParentDir",
			key:    "app/globals.css",
			backup: fileBackup{RealParentDir: "/Users/example/project/app", BackupFileName: "90c2@v2", Version: 2},
			want:   "/Users/example/project/app/globals.css",
		},
		{
			name:   "absolute key left alone",
			key:    "/tmp/scratch/probe.ts",
			backup: fileBackup{RealParentDir: "/tmp/scratch"},
			want:   "/tmp/scratch/probe.ts",
		},
		{
			name:   "missing realParentDir falls back to the key",
			key:    ".env",
			backup: fileBackup{},
			want:   ".env",
		},
		{
			name:   "bare key with a parent dir",
			key:    ".env",
			backup: fileBackup{RealParentDir: "/Users/example/project"},
			want:   "/Users/example/project/.env",
		},
	}
	for _, c := range cases {
		if got := c.backup.absPath(c.key); got != c.want {
			t.Errorf("%s: absPath(%q) = %q, want %q", c.name, c.key, got, c.want)
		}
	}
}
