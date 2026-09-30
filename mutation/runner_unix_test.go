//go:build unix

package mutation

import (
	"bytes"
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

// fakeGo writes an executable shell script named "go" that stands in for the
// go command, and returns its path.
func fakeGo(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// realTempDir returns a temp dir with symlinks resolved, the form the
// pipeline passes as RunRequest.ProjectRoot.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func readRecord(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	record := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, _ := strings.Cut(line, "=")
		record[key] = value
	}
	return record
}

func TestGoTestRunnerCommandLineAndEnvironment(t *testing.T) {
	t.Setenv("GOFLAGS", "-count=1")
	record := filepath.Join(t.TempDir(), "record")
	goBin := fakeGo(t, `{ echo "args=$*"; echo "pwd=$PWD"; echo "real=$(pwd -P)"; echo "goflags=[$GOFLAGS]"; echo "ci=$CI"; } > "`+record+`"`)
	root := realTempDir(t)

	result, err := (&GoTestRunner{GoBinary: goBin}).Run(context.Background(), RunRequest{
		ProjectRoot: root, Packages: "./pkg/...", OverlayPath: "/o/overlay.json",
	})
	if err != nil || result.ExitCode != 0 || result.TimedOut || result.BuildFailed {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	got := readRecord(t, record)
	want := map[string]string{
		"args":    "test -overlay=/o/overlay.json -vet=off -failfast ./pkg/...",
		"pwd":     root,
		"real":    root,
		"goflags": "[]",
		"ci":      "1",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
}

func TestGoTestRunnerWithoutOverlayAndGoFromPath(t *testing.T) {
	record := filepath.Join(t.TempDir(), "record")
	goBin := fakeGo(t, `echo "args=$*" > "`+record+`"`)
	t.Setenv("PATH", filepath.Dir(goBin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := (&GoTestRunner{}).Run(context.Background(), RunRequest{ProjectRoot: realTempDir(t), Packages: "./..."}); err != nil {
		t.Fatal(err)
	}
	if got := readRecord(t, record)["args"]; got != "test -vet=off -failfast ./..." {
		t.Errorf("args = %q", got)
	}
}

func TestGoTestRunnerExitCodesAndBuildFailures(t *testing.T) {
	cases := []struct {
		script      string
		exitCode    int
		buildFailed bool
	}{
		{`exit 0`, 0, false},
		{`echo "--- FAIL: TestMax"; echo "FAIL	example.com/x	0.2s"; exit 1`, 1, false},
		{`echo "FAIL	example.com/x [build failed]"; exit 1`, 1, true},
		{`echo "FAIL	example.com/x [setup failed]" >&2; exit 2`, 2, true},
		// The summary is the last output, with no newline: Run flushes it.
		{`printf 'FAIL\texample.com/x [build failed]'; exit 1`, 1, true},
		// A test that logs fixture text containing a marker is still a kill.
		{`echo "    x_test.go:3: FAIL	example.com/x [build failed]"; echo "FAIL	example.com/x	0.1s"; exit 1`, 1, false},
	}
	for _, c := range cases {
		result, err := (&GoTestRunner{GoBinary: fakeGo(t, c.script)}).Run(context.Background(), RunRequest{ProjectRoot: realTempDir(t), Packages: "./..."})
		if err != nil {
			t.Fatalf("%s: %v", c.script, err)
		}
		if result.ExitCode != c.exitCode || result.BuildFailed != c.buildFailed || result.TimedOut {
			t.Errorf("%s: result = %+v", c.script, result)
		}
		if c.exitCode != 0 && !strings.Contains(result.Output, "FAIL") {
			t.Errorf("%s: output tail = %q", c.script, result.Output)
		}
	}
}

// hangingGo stands in for a go test whose test binary never finishes: the
// script starts a long sleep (the test binary) and waits for it. The sleep's
// pid is written to the returned file.
func hangingGo(t *testing.T) (goBin, pidFile string) {
	t.Helper()
	pidFile = filepath.Join(t.TempDir(), "child.pid")
	return fakeGo(t, `sleep 120 & echo $! > "`+pidFile+`"; wait`), pidFile
}

func TestGoTestRunnerTimeoutKillsTheProcessGroup(t *testing.T) {
	goBin, pidFile := hangingGo(t)
	started := time.Now()
	result, err := (&GoTestRunner{GoBinary: goBin}).Run(context.Background(), RunRequest{
		ProjectRoot: realTempDir(t), Packages: "./...", Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut {
		t.Errorf("result = %+v, want TimedOut", result)
	}
	if elapsed := time.Since(started); elapsed > 60*time.Second {
		t.Errorf("a 1s timeout took %v", elapsed)
	}
	assertProcessGone(t, readPID(t, pidFile))
}

func TestGoTestRunnerCancelKillsTheProcessGroup(t *testing.T) {
	goBin, pidFile := hangingGo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		waitForFile(pidFile)
		cancel()
	}()
	_, err := (&GoTestRunner{GoBinary: goBin}).Run(ctx, RunRequest{ProjectRoot: realTempDir(t), Packages: "./..."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	assertProcessGone(t, readPID(t, pidFile))
}

func TestGoTestRunnerLaunchFailure(t *testing.T) {
	_, err := (&GoTestRunner{GoBinary: "/no/such/go-binary"}).Run(context.Background(), RunRequest{ProjectRoot: realTempDir(t), Packages: "./..."})
	var se *core.SlopguardError
	if !errors.As(err, &se) || se.Code != core.ErrRunnerUnavailable || !strings.Contains(se.Message, "could not launch '/no/such/go-binary'") {
		t.Errorf("err = %v, want runner_unavailable", err)
	}
}

func TestGoTestRunnerStreamsOutputOnlyWhenVerbose(t *testing.T) {
	goBin := fakeGo(t, `echo "ok  	example.com/x	(cached)"`)
	for _, verbosity := range []core.Verbosity{core.Normal, core.Verbose} {
		var buf bytes.Buffer
		result, err := (&GoTestRunner{GoBinary: goBin}).Run(context.Background(), RunRequest{
			ProjectRoot: realTempDir(t), Packages: "./...", Progress: core.NewReporter(&buf, verbosity),
		})
		if err != nil || !strings.Contains(result.Output, "(cached)") {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
		if streamed := strings.Contains(buf.String(), "(cached)"); streamed != (verbosity == core.Verbose) {
			t.Errorf("verbosity %d: streamed = %v", verbosity, streamed)
		}
	}
}

func waitForFile(path string) {
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			return
		}
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	waitForFile(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// assertProcessGone fails unless process pid exits within 15 seconds.
func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("process %d (the fake test binary) outlived the killed process group", pid)
}
