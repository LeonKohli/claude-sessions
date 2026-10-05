package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LeonKohli/claude-sessions/internal/util"
)

var ErrIndexBusy = errors.New("session index busy: another agent-sessions process holds the index lock")

// Serialize indexing and search across processes sharing a cache directory.
// Closing the descriptor, including on process exit, releases the lock.
func acquireWork(ctx context.Context) (*os.File, error) {
	dir, err := util.CacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "work.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeoutCause(ctx, 5*time.Second, ErrIndexBusy)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, context.Cause(ctx)
		}
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("acquire scan lock: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, context.Cause(ctx)
		case <-timer.C:
		}
	}
}
