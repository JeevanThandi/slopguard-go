//go:build unix

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeGoOnPath puts a shell script named go first on PATH for this test.
func fakeGoOnPath(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// killsEveryMutant passes the plain baseline (an empty overlay) and fails
// every run whose overlay replaces a file. $2 is -overlay=<config>.
const killsEveryMutant = `if grep -q '"Replace":{}' "${2#-overlay=}"; then exit 0; fi
echo "--- FAIL: TestMax"
exit 1`

func TestMutateFailUnderExitsTwoBelowTheThreshold(t *testing.T) {
	fakeGoOnPath(t, "exit 0") // every mutant survives: score 0
	root := mutateModule(t)
	stdout, stderr, code := run(t, "mutate", "--path", root, "--no-coverage", "--fail-under", "50")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "slopguard-go: mutation score 0.00% is below --fail-under 50.00\n") {
		t.Errorf("stderr should explain the failure: %q", stderr)
	}
	for _, want := range []string{"Survived (3)", "score:          0.00%", "Every tested mutant survived."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report missing %q:\n%s", want, stdout)
		}
	}
}

func TestMutateFailUnderPassesAtOrAboveTheThreshold(t *testing.T) {
	fakeGoOnPath(t, killsEveryMutant)
	root := mutateModule(t)
	stdout, stderr, code := run(t, "mutate", "--path", root, "--no-coverage", "--fail-under", "100", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, `"mutationScore": 100,`) || !strings.Contains(stdout, `"runner": "go test"`) {
		t.Errorf("report:\n%s", stdout)
	}
}

func TestMutateInterruptExitsWithTheSignalCode(t *testing.T) {
	for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143} {
		pidFile := filepath.Join(t.TempDir(), "child.pid")
		// The baseline never finishes on its own; its child is the stand-in
		// for a hanging test binary.
		fakeGoOnPath(t, `sleep 120 & echo $! > "`+pidFile+`"; wait`)
		send := fakeSignals(t)
		go func() {
			for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if data, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(data)) != "" {
					break
				}
			}
			send(sig)
		}()
		started := time.Now()
		stdout, stderr, code := run(t, "mutate", "--path", mutateModule(t), "--no-coverage")
		if code != want {
			t.Errorf("%v: exit = %d, want %d (stderr %q)", sig, code, want, stderr)
		}
		if !strings.Contains(stderr, "slopguard: interrupted\n") || stdout != "" {
			t.Errorf("%v: stderr = %q, stdout = %q", sig, stderr, stdout)
		}
		if elapsed := time.Since(started); elapsed > 60*time.Second {
			t.Errorf("%v: the interrupt took %v", sig, elapsed)
		}
		data, _ := os.ReadFile(pidFile)
		child, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatalf("no child pid: %v", err)
		}
		assertGone(t, child)
	}
}

func assertGone(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("process %d outlived the interrupted run", pid)
}
