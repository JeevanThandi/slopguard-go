//go:build !unix

package procgroup

import "os/exec"

// setProcessGroup does nothing: process groups are a unix feature.
func setProcessGroup(*exec.Cmd) {}

// killGroup kills the direct child only.
func killGroup(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
