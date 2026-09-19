//go:build unix

package cmd

import (
	"os/exec"
	"syscall"
)

// killWithChildren makes cmd run in its own process group and, when its context
// ends, kills that whole group. `sh -c` starts the real work as a child, and
// killing only the shell would leave that child running -- for an eval runner, a
// model call that keeps going (and holding its pipes open) after we gave up.
func killWithChildren(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
