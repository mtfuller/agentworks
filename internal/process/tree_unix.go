//go:build unix

package process

import (
	"errors"
	"os/exec"
	"syscall"
)

type unixTree struct{ pid int }

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachTree(cmd *exec.Cmd) (treeController, error) {
	return unixTree{pid: cmd.Process.Pid}, nil
}

func (tree unixTree) Graceful() error { return ignoreGone(syscall.Kill(-tree.pid, syscall.SIGTERM)) }
func (tree unixTree) Kill() error     { return ignoreGone(syscall.Kill(-tree.pid, syscall.SIGKILL)) }
func (tree unixTree) Close() error    { return nil }

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
