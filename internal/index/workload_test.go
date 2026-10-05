package index

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LeonKohli/claude-sessions/internal/session"
)

func TestWorkloadProcess(t *testing.T) {
	if os.Getenv("AGENT_SESSIONS_LOCK_CHILD") != "1" {
		return
	}
	lock, err := acquireWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := os.Stdout.WriteString("locked\n"); err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, os.Stdin)
}

// The child holds the shared admission lock to simulate another active scan.
func startWorkloadProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkloadProcess$")
	cmd.Env = append(os.Environ(), "AGENT_SESSIONS_LOCK_CHILD=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		ready <- scanner.Scan() && scanner.Text() == "locked"
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("lock owner did not start")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock owner did not become ready")
	}
	return cmd
}

func TestSearchWaitingForAnotherProcessCanBeCancelled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	startWorkloadProcess(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	entries := []session.SessionEntry{{FullPath: filepath.Join(t.TempDir(), "missing.jsonl")}}
	_, err := SearchSessions(ctx, entries, "needle", 0, 80)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting search returned %v instead of cancellation", err)
	}
}

func TestSearchSucceedsAfterLockOwnerIsKilled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	owner := startWorkloadProcess(t)
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	owner.Wait()
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"needle"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entries := []session.SessionEntry{{SessionID: "after-crash", FullPath: path}}
	hits, err := SearchSessions(ctx, entries, "needle", 0, 80)
	if err != nil || len(hits) != 1 || hits[0].Session.SessionID != "after-crash" || hits[0].Matches != 1 {
		t.Fatalf("search after crash = %+v, %v", hits, err)
	}
}
