//go:build unix

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func studioSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
