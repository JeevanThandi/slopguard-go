//go:build unix

package procgroup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrepareKillsTheWholeGroupWhenTheContextIsDone(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The shell stands in for `go test`; its background sleep stands in for
	// the test binary that go test starts.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `sleep 60 & echo $! > "$1"; wait`, "sh", pidFile)
	Prepare(cmd)
	if !cmd.SysProcAttr.Setpgid || cmd.WaitDelay != WaitDelay {
		t.Fatal("Prepare should request a new process group and a wait delay")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	child := waitForPIDFile(t, pidFile)
	cancel()
	started := time.Now()
	_ = cmd.Wait()
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Errorf("Wait took %v after cancel", elapsed)
	}
	waitForExit(t, child)
}

func TestKillGroupReportsAFinishedGroup(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "exit 0")
	Prepare(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := killGroup(cmd); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killGroup after exit = %v, want os.ErrProcessDone", err)
	}
}

// waitForPIDFile waits for a test helper process to write its pid to path.
func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no pid written to %s", path)
	return 0
}

// waitForExit fails the test unless process pid is gone within 15 seconds.
func waitForExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("process %d outlived its killed process group", pid)
}
