//go:build !unix && !windows

package process

import (
	"os"
	"os/exec"
)

type singleProcessTree struct{ process *os.Process }

func prepareCommand(cmd *exec.Cmd) {}

func attachTree(cmd *exec.Cmd) (treeController, error) {
	return singleProcessTree{process: cmd.Process}, nil
}

func (tree singleProcessTree) Graceful() error { return tree.process.Signal(os.Interrupt) }
func (tree singleProcessTree) Kill() error     { return tree.process.Kill() }
func (tree singleProcessTree) Close() error    { return nil }
