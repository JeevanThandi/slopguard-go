package core

import "fmt"

// ErrorCode is a stable, machine-readable classifier carried by every
// SlopguardError so that CLI --json output stays consumable by agents without
// pattern-matching on free-text messages.
type ErrorCode string

const (
	ErrFileNotFound        ErrorCode = "file_not_found"
	ErrNotADirectory       ErrorCode = "not_a_directory"
	ErrUnreadableFile      ErrorCode = "unreadable_file"
	ErrParseFailed         ErrorCode = "parse_failed"
	ErrCoverageDataMissing ErrorCode = "coverage_data_missing"
	ErrProjectRootMissing  ErrorCode = "project_root_not_found"
	ErrTestRunFailed       ErrorCode = "test_run_failed"
	ErrCoverageDecode      ErrorCode = "coverage_decode_failed"
	ErrInvalidArgument     ErrorCode = "invalid_argument"
	ErrUnsupported         ErrorCode = "unsupported"
	ErrInternal            ErrorCode = "internal_error"
	ErrRunnerUnavailable   ErrorCode = "runner_unavailable"
	ErrBaselineFailed      ErrorCode = "baseline_failed"
	// ErrMutationInProgress and ErrRestoreFailed complete the mutate error
	// catalog shared with the sibling ports, which edit source files in place.
	// The Go port never returns them: mutate hands each mutant to
	// `go test -overlay` and never writes to the source tree.
	ErrMutationInProgress ErrorCode = "mutation_in_progress"
	ErrRestoreFailed      ErrorCode = "restore_failed"
)

// SlopguardError is the typed error returned throughout slopguard-go. The Code
// is stable across releases; Message is human-facing.
type SlopguardError struct {
	Code    ErrorCode
	Message string
}

func (e *SlopguardError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func newErr(code ErrorCode, format string, args ...any) *SlopguardError {
	return &SlopguardError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func FileNotFound(path string) *SlopguardError {
	return newErr(ErrFileNotFound, "File not found: %s", path)
}

func NotADirectory(path string) *SlopguardError {
	return newErr(ErrNotADirectory, "Not a directory: %s", path)
}

func UnreadableFile(path string, underlying error) *SlopguardError {
	return newErr(ErrUnreadableFile, "Could not read %s: %v", path, underlying)
}

func ParseFailed(path string, underlying error) *SlopguardError {
	return newErr(ErrParseFailed, "Failed to parse %s: %v", path, underlying)
}

// ProjectRootNotFound reports that analyze found no go.mod to run go test in.
// analyze --no-coverage runs no tests, so the hint offers it.
func ProjectRootNotFound(searchedFrom string) *SlopguardError {
	return projectRootNotFound(searchedFrom,
		"Pass --project-dir to point at the module root, or --no-coverage to skip the test run.")
}

// MutateProjectRootNotFound reports that mutate found no go.mod to run go test
// in. mutate runs the tests even with --no-coverage, so the hint names only
// --project-dir.
func MutateProjectRootNotFound(searchedFrom string) *SlopguardError {
	return projectRootNotFound(searchedFrom, "Pass --project-dir to point at the module root.")
}

func projectRootNotFound(searchedFrom, hint string) *SlopguardError {
	return newErr(ErrProjectRootMissing, "No go.mod found at or above %s. %s", searchedFrom, hint)
}

func TestRunFailed(exitCode int, output string) *SlopguardError {
	return newErr(ErrTestRunFailed,
		"go test failed before coverage was produced (exit %d): %s", exitCode, output)
}

func CoverageDecodeFailed(underlying error) *SlopguardError {
	return newErr(ErrCoverageDecode, "Failed to decode coverage data: %v", underlying)
}

func InvalidArgument(name, reason string) *SlopguardError {
	return newErr(ErrInvalidArgument, "Invalid argument '%s': %s", name, reason)
}

func Unsupported(reason string) *SlopguardError {
	return newErr(ErrUnsupported, "Unsupported: %s", reason)
}

// RunnerUnavailable reports that the test runner could not be launched at all.
func RunnerUnavailable(reason string) *SlopguardError {
	return newErr(ErrRunnerUnavailable, "Test runner is unavailable: %s", reason)
}

// BaselineFailed reports that the unmutated test suite fails, so mutate cannot
// tell a killed mutant from a test that was already failing.
func BaselineFailed(exitCode int, output string) *SlopguardError {
	return newErr(ErrBaselineFailed,
		"The test suite fails without any mutation (exit %d). Fix the failing tests first: %s", exitCode, output)
}

// ErrorEnvelope is the JSON-friendly shape emitted on the CLI's --json error
// path: {"error": {"code": ..., "message": ...}}.
type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EnvelopeFor converts any error into a stable envelope. Non-SlopguardError
// values are reported as internal_error.
func EnvelopeFor(err error) ErrorEnvelope {
	var se *SlopguardError
	if asSlopguardError(err, &se) {
		return ErrorEnvelope{Code: string(se.Code), Message: se.Message}
	}
	return ErrorEnvelope{Code: string(ErrInternal), Message: err.Error()}
}

// asSlopguardError is a tiny errors.As shim kept local so this file has no
// import beyond fmt; it walks the simplest unwrap chain we ever produce.
func asSlopguardError(err error, target **SlopguardError) bool {
	for err != nil {
		if se, ok := err.(*SlopguardError); ok {
			*target = se
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
