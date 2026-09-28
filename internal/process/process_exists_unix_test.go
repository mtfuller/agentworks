//go:build unix

package process

import (
	"errors"
	"syscall"
)

const supportsTreeTests = true

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
