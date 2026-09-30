package coverage

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JeevanThandi/slopguard-go/core"
	"github.com/JeevanThandi/slopguard-go/internal/procgroup"
)

// Runner drives a test suite to produce a coverage profile. The concrete
// implementation is TestRunner; the interface exists so the pipeline can be
// tested without spawning real subprocesses.
type Runner interface {
	RunTests(projectRoot, coverageDir, packages string, progress *core.ProgressReporter) (TestOutcome, error)
}

// TestOutcome reports where coverage landed after a test run.
type TestOutcome struct {
	// ExitCode is the go test exit code: 0 when every test passed.
	ExitCode int
	// ProfilePath is the produced coverage profile, or "" if the run finished
	// without producing one.
	ProfilePath string
	// TestsPassed is false when some tests failed but coverage was still
	// emitted (a partial run is still useful).
	TestsPassed bool
}

// TestRunner drives the project's own `go test` to PRODUCE a coverage profile
// — the analog of slopguard-swift driving `xcodebuild test`. It owns that step:
// invoke `go test` with flags that force a coverage profile into a
// slopguard-owned directory and hand the path back for indexing.
type TestRunner struct {
	// GoBinary is the go command to invoke; defaults to "go" on PATH.
	GoBinary string
	// Context, when set, stops the run once it is done: go test then runs in
	// its own process group, and the whole group is killed. mutate sets it so
	// an interrupt also stops its coverage baseline; analyze leaves it nil.
	Context context.Context
}

// RunTests runs `go test -coverprofile=<dir>/cover.out -covermode=count
// -coverpkg=<packages> <packages>` in projectRoot.
//
// A non-zero exit WITH a profile present means tests failed but coverage was
// still emitted — keep going. A non-zero exit with NO profile means the run
// itself broke (build failure, vet error) — abort with the output tail.
func (r *TestRunner) RunTests(projectRoot, coverageDir, packages string, progress *core.ProgressReporter) (TestOutcome, error) {
	goBin := r.GoBinary
	if goBin == "" {
		goBin = "go"
	}
	profilePath := filepath.Join(coverageDir, "cover.out")
	args := []string{
		"test",
		"-coverprofile=" + profilePath,
		"-covermode=count",
		"-coverpkg=" + packages,
		packages,
	}

	progress.Phase("running go test with coverage in " + projectRoot + " — this can take a while")

	var cmd *exec.Cmd
	if r.Context != nil {
		cmd = exec.CommandContext(r.Context, goBin, args...)
		procgroup.Prepare(cmd)
	} else {
		cmd = exec.Command(goBin, args...)
	}
	cmd.Dir = projectRoot
	// Inherit the environment so the user's GOFLAGS/GOCACHE/proxy settings
	// apply; force non-interactive, predictable output.
	cmd.Env = append(os.Environ(), "GOFLAGS=", "CI=1")

	var tail bytes.Buffer
	sink := &tailWriter{buf: &tail, limit: 8 * 1024, progress: progress}
	cmd.Stdout = sink
	cmd.Stderr = sink

	err := cmd.Run()
	// A profile is only "produced" if it carries at least one coverage block.
	// On a build failure `go test` still writes a header-only profile (just the
	// `mode:` line), which must not be mistaken for a partial-but-usable run.
	produced := profileHasData(profilePath)

	if err == nil {
		if produced {
			return TestOutcome{ProfilePath: profilePath, TestsPassed: true}, nil
		}
		return TestOutcome{TestsPassed: true}, nil
	}

	exitCode := 1
	var ee *exec.ExitError
	if asExitError(err, &ee) {
		exitCode = ee.ExitCode()
	} else {
		// The go binary itself couldn't be launched (not on PATH, etc.).
		return TestOutcome{}, core.Unsupported("could not launch '" + goBin + "': " + err.Error())
	}

	if produced {
		return TestOutcome{ExitCode: exitCode, ProfilePath: profilePath, TestsPassed: false}, nil
	}
	out := tail.String()
	if out == "" {
		out = "no output captured"
	}
	return TestOutcome{}, core.TestRunFailed(exitCode, out)
}

// profileHasData reports whether the profile at path contains at least one
// coverage block line (i.e. more than the bare `mode:` header).
func profileHasData(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		return true
	}
	return false
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// tailWriter keeps a bounded tail of subprocess output for error reporting and
// streams it through under --verbose.
type tailWriter struct {
	buf      *bytes.Buffer
	limit    int
	progress *core.ProgressReporter
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.progress.Raw(p)
	w.buf.Write(p)
	if w.buf.Len() > w.limit {
		// Drop the oldest bytes beyond the limit.
		excess := w.buf.Len() - w.limit
		w.buf.Next(excess)
	}
	return len(p), nil
}
