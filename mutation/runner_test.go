package mutation

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/JeevanThandi/slopguard-go/core"
)

func TestOutputSinkDetectsBuildFailureSummaryLines(t *testing.T) {
	long := strings.Repeat("x", maxSummaryLine)
	cases := []struct {
		name   string
		writes []string
		want   bool
	}{
		{"test failure", []string{"--- FAIL: TestX\nFAIL\texample.com/x\t0.1s\n"}, false},
		{"build failed", []string{"FAIL\texample.com/x [build failed]\n"}, true},
		{"setup failed", []string{"ok  \texample.com/y\t(cached)\nFAIL\texample.com/x [setup failed]\n"}, true},
		{"CRLF line ending", []string{"FAIL\texample.com/x [build failed]\r\n"}, true},
		{"split across writes", []string{"FAIL\texample.com/x [bui", "ld failed]\n"}, true},
		{"split across three writes", []string{"FAIL\texample.com/x [build", " ", "failed]\n"}, true},
		{"split before the newline", []string{"FAIL\texample.com/x [setup failed]", "\nok\n"}, true},
		{"final line without a newline", []string{"ok\n", "FAIL\texample.com/x [build failed]"}, true},
		{"summary then more output", []string{"FAIL\texample.com/x [build failed]\n", strings.Repeat("ok\n", 5000)}, true},
		// A failing test that logs fixture text prints it indented: still a kill.
		{"indented test log", []string{"    x_test.go:12: FAIL\texample.com/x [build failed]\n"}, false},
		{"bare marker", []string{"[build failed]\n"}, false},
		{"marker inside a line", []string{"FAIL\texample.com/x [build failed] (fixture)\n"}, false},
		{"marker split by a newline", []string{"FAIL\texample.com/x [build\nfailed]\n"}, false},
		// A line longer than any summary line is dropped; the next line counts.
		{"overlong line", []string{"FAIL\t" + long + " [build failed]\n"}, false},
		{"overlong line across writes", []string{"FAIL\t" + long, long, " [build failed]\n"}, false},
		{"summary after an overlong line", []string{"FAIL\t" + long + " [build failed]\n", "FAIL\tx [setup failed]\n"}, true},
	}
	for _, c := range cases {
		sink := &outputSink{progress: core.SilentReporter()}
		for _, w := range c.writes {
			if n, err := sink.Write([]byte(w)); n != len(w) || err != nil {
				t.Fatalf("%s: Write = %d, %v", c.name, n, err)
			}
		}
		sink.flush()
		if sink.buildFailed != c.want {
			t.Errorf("%s: buildFailed = %v, want %v", c.name, sink.buildFailed, c.want)
		}
	}
}

func TestOutputSinkKeepsABoundedTail(t *testing.T) {
	sink := &outputSink{progress: core.SilentReporter()}
	var all strings.Builder
	for i := 0; i < 3; i++ {
		chunk := strings.Repeat(string(rune('a'+i)), 5000)
		all.WriteString(chunk)
		sink.Write([]byte(chunk))
	}
	tail := sink.tail.String()
	if len(tail) != outputLimit || !strings.HasSuffix(all.String(), tail) {
		t.Errorf("tail has %d bytes, want the last %d", len(tail), outputLimit)
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		result RunResult
		want   core.MutantStatus
	}{
		{RunResult{TimedOut: true, ExitCode: -1}, core.StatusTimeout},
		{RunResult{ExitCode: 0}, core.StatusSurvived},
		{RunResult{ExitCode: 1, BuildFailed: true}, core.StatusCompileError},
		{RunResult{ExitCode: 1}, core.StatusKilled},
		{RunResult{ExitCode: 2}, core.StatusKilled},
	}
	for _, c := range cases {
		if got := statusOf(c.result); got != c.want {
			t.Errorf("statusOf(%+v) = %s, want %s", c.result, got, c.want)
		}
	}
}

func TestSecondsToDuration(t *testing.T) {
	cases := map[float64]time.Duration{
		2.5:  2500 * time.Millisecond,
		34:   34 * time.Second,
		1e10: time.Duration(math.MaxInt64), // would overflow int64 nanoseconds
	}
	for seconds, want := range cases {
		if got := secondsToDuration(seconds); got != want {
			t.Errorf("secondsToDuration(%v) = %v, want %v", seconds, got, want)
		}
	}
}
