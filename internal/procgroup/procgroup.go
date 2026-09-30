// Package procgroup runs a command in its own process group, so that a
// timeout or an interrupt stops the command together with every process it
// started. `go test` runs each test binary as a child process; killing only
// the go command would leave an infinite-loop test binary running.
package procgroup

import (
	"os/exec"
	"time"
)

// WaitDelay bounds how long Wait keeps reading a command's output after the
// command exits or is killed, in case a leftover child holds the pipes open.
const WaitDelay = 10 * time.Second

// Prepare configures cmd, which must come from exec.CommandContext, to start
// in a new process group (on unix) and to kill that whole group when the
// context is done. On other platforms it kills the direct child only.
func Prepare(cmd *exec.Cmd) {
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = WaitDelay
}
