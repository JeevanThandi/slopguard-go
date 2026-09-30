package core

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorConstructorsCarryCodes(t *testing.T) {
	cases := []struct {
		err  *SlopguardError
		code ErrorCode
	}{
		{FileNotFound("/x"), ErrFileNotFound},
		{NotADirectory("/x"), ErrNotADirectory},
		{UnreadableFile("/x", errors.New("boom")), ErrUnreadableFile},
		{ParseFailed("/x", errors.New("boom")), ErrParseFailed},
		{ProjectRootNotFound("/x"), ErrProjectRootMissing},
		{MutateProjectRootNotFound("/x"), ErrProjectRootMissing},
		{TestRunFailed(2, "tail"), ErrTestRunFailed},
		{CoverageDecodeFailed(errors.New("boom")), ErrCoverageDecode},
		{InvalidArgument("--t", "nope"), ErrInvalidArgument},
		{Unsupported("nope"), ErrUnsupported},
		{RunnerUnavailable("nope"), ErrRunnerUnavailable},
		{BaselineFailed(1, "tail"), ErrBaselineFailed},
	}
	for _, c := range cases {
		if c.err.Code != c.code {
			t.Errorf("code = %q, want %q", c.err.Code, c.code)
		}
		if c.err.Message == "" {
			t.Errorf("%s: empty message", c.code)
		}
		if !strings.Contains(c.err.Error(), string(c.code)) {
			t.Errorf("Error() %q should contain code %q", c.err.Error(), c.code)
		}
	}
}

func TestEnvelopeForNonSlopguardError(t *testing.T) {
	env := EnvelopeFor(errors.New("plain"))
	if env.Code != string(ErrInternal) || env.Message != "plain" {
		t.Errorf("envelope = %+v", env)
	}
}

func TestEnvelopeForWrappedError(t *testing.T) {
	wrapped := wrap(FileNotFound("/x"))
	env := EnvelopeFor(wrapped)
	if env.Code != string(ErrFileNotFound) {
		t.Errorf("wrapped envelope code = %q, want file_not_found", env.Code)
	}
}

func TestAsSlopguardErrorStopsAtNonUnwrappable(t *testing.T) {
	var se *SlopguardError
	if asSlopguardError(errors.New("plain"), &se) {
		t.Error("plain error should not resolve to a SlopguardError")
	}
}

// wrap returns an error whose Unwrap() yields the given cause.
type wrapped struct{ cause error }

func (w wrapped) Error() string { return "wrapped: " + w.cause.Error() }
func (w wrapped) Unwrap() error { return w.cause }

func wrap(cause error) error { return wrapped{cause} }

func TestMutateErrorWording(t *testing.T) {
	// The wording is shared with the sibling ports.
	if got := BaselineFailed(2, "FAIL x").Message; got != "The test suite fails without any mutation (exit 2). Fix the failing tests first: FAIL x" {
		t.Errorf("BaselineFailed message = %q", got)
	}
	if got := RunnerUnavailable("could not launch 'go'").Message; got != "Test runner is unavailable: could not launch 'go'" {
		t.Errorf("RunnerUnavailable message = %q", got)
	}
	// mutate runs the tests even with --no-coverage, so only analyze's
	// message offers that flag.
	if got := ProjectRootNotFound("/x").Message; got != "No go.mod found at or above /x. Pass --project-dir to point at the module root, or --no-coverage to skip the test run." {
		t.Errorf("ProjectRootNotFound message = %q", got)
	}
	if got := MutateProjectRootNotFound("/x").Message; got != "No go.mod found at or above /x. Pass --project-dir to point at the module root." {
		t.Errorf("MutateProjectRootNotFound message = %q", got)
	}
	codes := map[ErrorCode]string{
		ErrRunnerUnavailable:  "runner_unavailable",
		ErrBaselineFailed:     "baseline_failed",
		ErrMutationInProgress: "mutation_in_progress",
		ErrRestoreFailed:      "restore_failed",
	}
	for code, want := range codes {
		if string(code) != want {
			t.Errorf("code %q, want %q", code, want)
		}
	}
}
