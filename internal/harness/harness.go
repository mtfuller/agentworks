// Package harness defines the vendor-neutral boundary for headless agent CLIs.
package harness

import "context"

type ID string

const (
	ClaudeCode    ID = "claude-code"
	GitHubCopilot ID = "github-copilot"
)

type ProbeStatus string

const (
	ProbeMissing         ProbeStatus = "missing"
	ProbeUnauthenticated ProbeStatus = "unauthenticated"
	ProbeAuthUnverified  ProbeStatus = "auth-unverified"
	ProbeReady           ProbeStatus = "ready"
	ProbeIncompatible    ProbeStatus = "incompatible"
)

type Capability string

const (
	CapabilityHeadless        Capability = "headless"
	CapabilityStreaming       Capability = "streaming"
	CapabilityBoundedApproval Capability = "bounded-approval"
)

type ProbeResult struct {
	Harness      ID
	Status       ProbeStatus
	Executable   string
	Version      string
	AuthMethod   string
	Capabilities []Capability
	Diagnostics  []string
}

type Adapter interface {
	ID() ID
	Probe(context.Context) ProbeResult
}
