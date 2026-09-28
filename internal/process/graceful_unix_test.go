//go:build unix

package process

import (
	"os/signal"
	"syscall"
)

func ignoreGracefulSignal() {
	signal.Ignore(syscall.SIGTERM)
}
