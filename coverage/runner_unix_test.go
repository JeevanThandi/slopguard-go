//go:build unix

package coverage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/JeevanThandi/slopguard-go/core"
)

// fakeGoScript writes an executable shell script that stands in for go.
func fakeGoScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunnerReportsTheExitCodeOfAFailingRunWithCoverage(t *testing.T) {
	// $2 is -coverprofile=<path>: write a profile with data, then fail.
	goBin := fakeGoScript(t, `printf 'mode: count\nexample.com/m/a.go:1.1,2.2 1 1\n' > "${2#-coverprofile=}"; exit 3`)
	outcome, err := (&TestRunner{GoBinary: goBin}).RunTests(t.TempDir(), t.TempDir(), "./...", core.SilentReporter())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.ExitCode != 3 || outcome.TestsPassed || outcome.ProfilePath == "" {
		t.Errorf("outcome = %+v, want exit 3 with a profile", outcome)
	}
}

func TestRunnerContextKillsTheProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	goBin := fakeGoScript(t, `sleep 120 & echo $! > "`+pidFile+`"; wait`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pid := make(chan int, 1)
	go func() {
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if data, err := os.ReadFile(pidFile); err == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
					pid <- n
					cancel()
					return
				}
			}
		}
		pid <- 0
		cancel()
	}()
	started := time.Now()
	_, err := (&TestRunner{GoBinary: goBin, Context: ctx}).RunTests(t.TempDir(), t.TempDir(), "./...", core.SilentReporter())
	if err == nil {
		t.Error("a cancelled coverage run produced no profile, so it should fail")
	}
	if elapsed := time.Since(started); elapsed > 60*time.Second {
		t.Errorf("cancel took %v", elapsed)
	}
	child := <-pid
	if child == 0 {
		t.Fatal("the fake go test never started its child")
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := syscall.Kill(child, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	syscall.Kill(child, syscall.SIGKILL)
	t.Errorf("process %d outlived the cancelled coverage run", child)
}
