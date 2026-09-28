//go:build !unix && !windows

package process

const supportsTreeTests = false

func processExists(pid int) bool { return false }
