//go:build !unix && !windows

package process

func ignoreGracefulSignal() {}
