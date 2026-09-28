// Package providers validates and constructs host and Docker tool runtimes.
// It never invokes a shell and keeps provider policy explicit in the returned
// execution record.
package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/spec"
)

type State string

const (
	StateReady        State = "ready"
	StateMissing      State = "missing"
	StateIncompatible State = "incompatible"
)

type Readiness struct {
	Provider    spec.Provider `json:"provider"`
	State       State         `json:"state"`
	Runtime     string        `json:"runtime,omitempty"`
	Version     string        `json:"version,omitempty"`
	ImageDigest string        `json:"image_digest,omitempty"`
	Network     string        `json:"network"`
	Detail      string        `json:"detail,omitempty"`
}

type Probe struct {
	LookPath      func(string) (string, error)
	VersionOutput func(context.Context, string) ([]byte, error)
}

func (probe Probe) lookPath(name string) (string, error) {
	if probe.LookPath != nil {
		return probe.LookPath(name)
	}
	return exec.LookPath(name)
}

func (probe Probe) version(ctx context.Context, executable string) ([]byte, error) {
	if probe.VersionOutput != nil {
		return probe.VersionOutput(ctx, executable)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(checkCtx, executable, "--version").CombinedOutput()
}

// CheckHost verifies the command and every declared executable/version
// requirement without starting the tool itself.
func (probe Probe) CheckHost(ctx context.Context, variant spec.RuntimeVariant, workdir string) Readiness {
	result := Readiness{Provider: spec.ProviderHost, State: StateReady, Network: "host"}
	argv := variant.Argv()
	if len(argv) == 0 {
		result.State, result.Detail = StateIncompatible, "host command is empty"
		return result
	}
	command, err := probe.resolveExecutable(argv[0], workdir)
	if err != nil {
		result.State, result.Detail = StateMissing, fmt.Sprintf("executable %q was not found", argv[0])
		return result
	}
	result.Runtime = command
	for _, raw := range variant.Requires {
		requirement, err := parseRequirement(raw)
		if err != nil {
			result.State, result.Detail = StateIncompatible, err.Error()
			return result
		}
		executable, err := probe.lookPath(requirement.name)
		if err != nil {
			result.State, result.Detail = StateMissing, fmt.Sprintf("required executable %q was not found", requirement.name)
			return result
		}
		output, err := probe.version(ctx, executable)
		if err != nil {
			result.State, result.Detail = StateIncompatible, fmt.Sprintf("could not determine %s version", requirement.name)
			return result
		}
		version, ok := parseVersion(string(output))
		if !ok || !requirement.matches(version) {
			result.State = StateIncompatible
			result.Version = version
			result.Detail = fmt.Sprintf("%s %s does not satisfy %s", requirement.name, printableVersion(version), raw)
			return result
		}
		if requirement.name == filepath.Base(command) {
			result.Version = version
		}
	}
	return result
}

func (probe Probe) resolveExecutable(name, workdir string) (string, error) {
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(workdir, path)
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return "", errors.New("not executable")
		}
		return path, nil
	}
	return probe.lookPath(name)
}

// CheckDocker verifies a usable Docker CLI/daemon and reports the pinned
// image digest without pulling or starting the image.
func (probe Probe) CheckDocker(ctx context.Context, variant spec.RuntimeVariant) Readiness {
	result := Readiness{Provider: spec.ProviderContainer, Network: "policy-controlled", ImageDigest: ImageDigest(variant.Image)}
	docker, err := probe.lookPath("docker")
	if err != nil {
		result.State, result.Detail = StateMissing, "Docker executable was not found"
		return result
	}
	result.Runtime = docker
	if result.ImageDigest == "" {
		result.State, result.Detail = StateIncompatible, "container image is not pinned by sha256 digest"
		return result
	}
	var output []byte
	if probe.VersionOutput != nil {
		output, err = probe.VersionOutput(ctx, docker)
	} else {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		output, err = exec.CommandContext(checkCtx, docker, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	}
	if err != nil {
		result.State, result.Detail = StateIncompatible, "Docker daemon is unavailable"
		return result
	}
	result.State = StateReady
	result.Version = strings.TrimSpace(string(output))
	return result
}

type DockerPolicy struct {
	Workspace  string
	Permission spec.Permission
	// Env is the explicit list of variable names the container may inherit.
	Env []string
	// HostEnvironment is the exact parent environment used to source allowed
	// values. Nil uses os.Environ; tests and callers may provide a narrower set.
	HostEnvironment []string
	Network         bool
	Name            string
}

type Invocation struct {
	Process     awprocess.Spec `json:"-"`
	Provider    spec.Provider  `json:"provider"`
	Runtime     string         `json:"runtime"`
	Version     string         `json:"version,omitempty"`
	ImageDigest string         `json:"image_digest,omitempty"`
	Network     string         `json:"network"`
	Readonly    bool           `json:"readonly"`
	Environment []string       `json:"environment"`
}

var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// DockerInvocation constructs the complete container boundary. The workspace
// is its only host mount, the Docker socket is never mounted, and environment
// inheritance is limited to the named allowlist.
func DockerInvocation(docker string, version string, variant spec.RuntimeVariant, policy DockerPolicy) (Invocation, error) {
	workspace, err := filepath.Abs(policy.Workspace)
	if err != nil {
		return Invocation{}, err
	}
	if strings.Contains(workspace, ",") {
		return Invocation{}, errors.New("Docker workspace path cannot contain a comma")
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return Invocation{}, errors.New("Docker workspace must be an existing directory")
	}
	if ImageDigest(variant.Image) == "" {
		return Invocation{}, errors.New("Docker image must be pinned by sha256 digest")
	}
	readonly := policy.Permission == spec.PermissionReadonly
	mount := "type=bind,src=" + workspace + ",dst=/workspace"
	if readonly {
		mount += ",readonly"
	}
	args := []string{"run", "--rm", "--interactive", "--workdir", "/workspace", "--mount", mount,
		"--security-opt", "no-new-privileges", "--cap-drop", "ALL", "--pids-limit", "512"}
	if strings.TrimSpace(policy.Name) != "" {
		args = append(args, "--name", policy.Name)
	}
	if readonly {
		args = append(args, "--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=64m")
	}
	network := "default"
	if !policy.Network {
		network = "none"
		args = append(args, "--network", "none")
	}
	environment := append([]string(nil), policy.Env...)
	sortStrings(environment)
	for index, name := range environment {
		if !envPattern.MatchString(name) {
			return Invocation{}, fmt.Errorf("environment allowlist entry %q is invalid", name)
		}
		if index > 0 && environment[index-1] == name {
			continue
		}
		args = append(args, "--env", name)
	}
	args = append(args, variant.Image)
	args = append(args, variant.Argv()...)
	allowedValues := selectEnvironment(policy.HostEnvironment, environment)
	return Invocation{
		Process: awprocess.Spec{Executable: docker, Args: args, Dir: workspace, Env: allowedValues}, Provider: spec.ProviderContainer,
		Runtime: docker, Version: version, ImageDigest: ImageDigest(variant.Image), Network: network,
		Readonly: readonly, Environment: uniqueStrings(environment),
	}, nil
}

func selectEnvironment(source, allowed []string) []string {
	if source == nil {
		source = os.Environ()
	}
	allowedSet := map[string]bool{}
	for _, name := range allowed {
		allowedSet[name] = true
	}
	result := []string{}
	for _, item := range source {
		name, _, ok := strings.Cut(item, "=")
		if ok && allowedSet[name] {
			result = append(result, item)
		}
	}
	sortStrings(result)
	return result
}

func HostInvocation(readiness Readiness, variant spec.RuntimeVariant, workdir string, environment []string) (Invocation, error) {
	if readiness.Provider != spec.ProviderHost || readiness.State != StateReady {
		return Invocation{}, errors.New("host provider is not ready")
	}
	argv := variant.Argv()
	if len(argv) == 0 {
		return Invocation{}, errors.New("host command is empty")
	}
	return Invocation{Process: awprocess.Spec{Executable: readiness.Runtime, Args: argv[1:], Dir: workdir, Env: append([]string(nil), environment...)}, Provider: spec.ProviderHost, Runtime: readiness.Runtime, Version: readiness.Version, Network: "host", Environment: envNames(environment)}, nil
}

func ImageDigest(image string) string {
	index := strings.LastIndex(image, "@sha256:")
	if index < 0 || len(image[index+8:]) != 64 {
		return ""
	}
	return "sha256:" + strings.ToLower(image[index+8:])
}

type requirement struct {
	name, operator, version string
}

var requirementPattern = regexp.MustCompile(`^([A-Za-z0-9._+-]+)\s*(>=|<=|=|>|<)\s*v?([0-9]+(?:\.[0-9]+){0,3})$`)
var versionPattern = regexp.MustCompile(`(?i)\bv?([0-9]+(?:\.[0-9]+){0,3})\b`)

func parseRequirement(value string) (requirement, error) {
	parts := requirementPattern.FindStringSubmatch(strings.TrimSpace(value))
	if parts == nil {
		return requirement{}, fmt.Errorf("runtime requirement %q must look like executable>=version", value)
	}
	return requirement{name: parts[1], operator: parts[2], version: parts[3]}, nil
}

func parseVersion(output string) (string, bool) {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func (value requirement) matches(actual string) bool {
	comparison := compareVersions(actual, value.version)
	switch value.operator {
	case ">=":
		return comparison >= 0
	case "<=":
		return comparison <= 0
	case ">":
		return comparison > 0
	case "<":
		return comparison < 0
	default:
		return comparison == 0
	}
}

func compareVersions(left, right string) int {
	l, r := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(l) || index < len(r); index++ {
		var lv, rv int
		if index < len(l) {
			lv, _ = strconv.Atoi(l[index])
		}
		if index < len(r) {
			rv, _ = strconv.Atoi(r[index])
		}
		if lv < rv {
			return -1
		}
		if lv > rv {
			return 1
		}
	}
	return 0
}

func printableVersion(value string) string {
	if value == "" {
		return "has an unknown version"
	}
	return value
}

func envNames(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, value := range environment {
		name, _, ok := strings.Cut(value, "=")
		if ok && envPattern.MatchString(name) {
			result = append(result, name)
		}
	}
	sortStrings(result)
	return uniqueStrings(result)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
