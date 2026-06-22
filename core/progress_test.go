package core

import (
	"bytes"
	"strings"
	"testing"
)

func TestSilentReporterDiscards(t *testing.T) {
	r := SilentReporter()
	r.Phase("hello")
	r.Raw([]byte("raw"))
	if r.IsVerbose() {
		t.Error("silent reporter should not be verbose")
	}
}

func TestNilReporterIsSafe(t *testing.T) {
	var r *ProgressReporter
	r.Phase("x")
	r.Raw([]byte("x"))
	if r.IsVerbose() {
		t.Error("nil reporter should not be verbose")
	}
}

func TestNormalReporterEmitsPhaseNotRaw(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, Normal)
	r.Phase("walking")
	r.Raw([]byte("subprocess output"))
	out := buf.String()
	if !strings.Contains(out, "slopguard: walking") {
		t.Errorf("phase missing: %q", out)
	}
	if strings.Contains(out, "subprocess output") {
		t.Error("normal reporter must not stream raw output")
	}
	if r.IsVerbose() {
		t.Error("normal reporter is not verbose")
	}
}

func TestVerboseReporterStreamsRaw(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, Verbose)
	r.Raw([]byte("subprocess output"))
	if !strings.Contains(buf.String(), "subprocess output") {
		t.Error("verbose reporter should stream raw output")
	}
	if !r.IsVerbose() {
		t.Error("verbose reporter should report IsVerbose")
	}
}
