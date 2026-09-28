//go:build windows

package process

import (
	"os"
	"os/signal"
)

func ignoreGracefulSignal() {
	signal.Ignore(os.Interrupt)
}
