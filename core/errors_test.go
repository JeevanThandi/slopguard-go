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
		{TestRunFailed(2, "tail"), ErrTestRunFailed},
		{CoverageDecodeFailed(errors.New("boom")), ErrCoverageDecode},
		{InvalidArgument("--t", "nope"), ErrInvalidArgument},
		{Unsupported("nope"), ErrUnsupported},
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
