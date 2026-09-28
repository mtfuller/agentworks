package harness

import (
	"context"
	"time"
)

const ProbeTimeout = 5 * time.Second

func ProbeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, ProbeTimeout)
}

func NewProbeResult(id ID) ProbeResult {
	return ProbeResult{
		Harness:      id,
		Capabilities: []Capability{},
		Diagnostics:  []string{},
	}
}
