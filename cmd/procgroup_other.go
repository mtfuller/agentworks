//go:build !unix

package cmd

import "os/exec"

// killWithChildren has no portable equivalent off unix; the default behavior
// (kill the shell) applies. AgentWorks documents a POSIX shell as a requirement.
func killWithChildren(cmd *exec.Cmd) {}
