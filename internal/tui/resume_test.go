package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeonKohli/claude-sessions/internal/provider"
)

func TestResumeReportsLaunchFailures(t *testing.T) {
	for _, missingProject := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid executable", true: "missing project"}[missingProject], func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "codex"), []byte("not an executable format"), 0700); err != nil {
				t.Fatal(err)
			}
			project := root
			if missingProject {
				project = filepath.Join(root, "missing")
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestResumeHelper$", "--", project)
			cmd.Env = append(os.Environ(), "AGENT_SESSIONS_RESUME_HELPER=1", "PATH="+root)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "resume") {
				t.Fatalf("failed resume reported success: err=%v output=%s", err, out)
			}
		})
	}
}

func TestResumeHelper(t *testing.T) {
	if os.Getenv("AGENT_SESSIONS_RESUME_HELPER") != "1" {
		return
	}
	m := Model{resumeSessionID: "recorded-id", resumeProject: os.Args[len(os.Args)-1], resumeProvider: provider.Codex}
	if err := m.ExecResume(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
