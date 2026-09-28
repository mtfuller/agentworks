package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/spec"
)

const pinnedImage = "ghcr.io/example/tool@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHostReadinessDistinguishesMissingIncompatibleAndReady(t *testing.T) {
	variant := spec.RuntimeVariant{Provider: spec.ProviderHost, Command: spec.Command{"node", "server.js"}, Requires: []string{"node>=22"}}
	missing := Probe{LookPath: func(string) (string, error) { return "", errors.New("missing") }}.CheckHost(context.Background(), variant, t.TempDir())
	if missing.State != StateMissing {
		t.Fatalf("missing=%#v", missing)
	}
	probe := Probe{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		VersionOutput: func(context.Context, string) ([]byte, error) {
			return []byte("v20.11.0"), nil
		},
	}
	incompatible := probe.CheckHost(context.Background(), variant, t.TempDir())
	if incompatible.State != StateIncompatible || !strings.Contains(incompatible.Detail, "does not satisfy") {
		t.Fatalf("incompatible=%#v", incompatible)
	}
	probe.VersionOutput = func(context.Context, string) ([]byte, error) { return []byte("node v22.4.1"), nil }
	ready := probe.CheckHost(context.Background(), variant, t.TempDir())
	if ready.State != StateReady || ready.Version != "22.4.1" || ready.Runtime != "/bin/node" {
		t.Fatalf("ready=%#v", ready)
	}
}

func TestDockerInvocationAppliesIsolationPolicy(t *testing.T) {
	workspace := t.TempDir()
	variant := spec.RuntimeVariant{Provider: spec.ProviderContainer, Image: pinnedImage, Command: spec.Command{"/app/tool"}, Args: []string{"serve"}}
	invocation, err := DockerInvocation("/usr/bin/docker", "28.0.0", variant, DockerPolicy{
		Workspace: workspace, Permission: spec.PermissionReadonly, Env: []string{"TOKEN", "ACCOUNT", "TOKEN"},
		HostEnvironment: []string{"TOKEN=secret", "ACCOUNT=acme", "UNLISTED=nope"}, Network: false, Name: "agentworks-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(invocation.Process.Args, " ")
	for _, required := range []string{"--read-only", "readonly", "--network none", "--cap-drop ALL", "no-new-privileges", pinnedImage, "/app/tool serve"} {
		if !strings.Contains(args, required) {
			t.Errorf("args missing %q: %s", required, args)
		}
	}
	if strings.Contains(strings.ToLower(args), "docker.sock") || strings.Count(args, "type=bind") != 1 {
		t.Fatalf("unsafe mount policy: %s", args)
	}
	if !reflect.DeepEqual(invocation.Environment, []string{"ACCOUNT", "TOKEN"}) {
		t.Fatalf("environment=%#v", invocation.Environment)
	}
	if !reflect.DeepEqual(invocation.Process.Env, []string{"ACCOUNT=acme", "TOKEN=secret"}) || strings.Contains(args, "secret") {
		t.Fatalf("exact environment policy env=%#v args=%s", invocation.Process.Env, args)
	}
	if invocation.ImageDigest != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" || invocation.Network != "none" || !invocation.Readonly {
		t.Fatalf("record=%#v", invocation)
	}
}

func TestDockerInvocationReadwriteAndValidation(t *testing.T) {
	variant := spec.RuntimeVariant{Provider: spec.ProviderContainer, Image: pinnedImage, Command: spec.Command{"/app/tool"}}
	invocation, err := DockerInvocation("docker", "28", variant, DockerPolicy{Workspace: t.TempDir(), Permission: spec.PermissionReadwrite, Network: true})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(invocation.Process.Args, " ")
	if strings.Contains(args, "readonly") || strings.Contains(args, "--network none") || invocation.Readonly || invocation.Network != "default" {
		t.Fatalf("readwrite invocation=%#v", invocation)
	}
	if _, err := DockerInvocation("docker", "28", variant, DockerPolicy{Workspace: t.TempDir(), Env: []string{"BAD=VALUE"}}); err == nil {
		t.Fatal("invalid environment name accepted")
	}
}

func TestHostInvocationUsesExactEnvironment(t *testing.T) {
	variant := spec.RuntimeVariant{Provider: spec.ProviderHost, Command: spec.Command{"node"}, Args: []string{"server.js"}}
	readiness := Readiness{Provider: spec.ProviderHost, State: StateReady, Runtime: "/usr/bin/node", Version: "22.1", Network: "host"}
	invocation, err := HostInvocation(readiness, variant, "/workspace", []string{"TOKEN=secret", "PATH=/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Process.Executable != "/usr/bin/node" || !reflect.DeepEqual(invocation.Process.Args, []string{"server.js"}) || !reflect.DeepEqual(invocation.Process.Env, []string{"TOKEN=secret", "PATH=/bin"}) {
		t.Fatalf("invocation=%#v", invocation)
	}
	if !reflect.DeepEqual(invocation.Environment, []string{"PATH", "TOKEN"}) {
		t.Fatalf("environment names=%#v", invocation.Environment)
	}
}

func TestDockerSmoke(t *testing.T) {
	if os.Getenv("AGENTWORKS_DOCKER_SMOKE") != "1" {
		t.Skip("set AGENTWORKS_DOCKER_SMOKE=1 to probe the local Docker daemon")
	}
	readiness := (Probe{}).CheckDocker(context.Background(), spec.RuntimeVariant{Provider: spec.ProviderContainer, Image: pinnedImage})
	if readiness.State != StateReady {
		t.Fatalf("Docker is not ready: %#v", readiness)
	}
	image := os.Getenv("AGENTWORKS_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("set AGENTWORKS_DOCKER_TEST_IMAGE to a locally available digest-pinned image for isolation checks")
	}
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	variant := spec.RuntimeVariant{Provider: spec.ProviderContainer, Image: image, Command: spec.Command{"/bin/sh"}, Args: []string{"-c", "test ! -e /outside-secret"}}
	invocation, err := DockerInvocation(readiness.Runtime, readiness.Version, variant, DockerPolicy{Workspace: workspace, Permission: spec.PermissionReadonly, Network: false, Name: "agentworks-policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, runErr := awprocess.Run(ctx, invocation.Process); runErr != nil {
		t.Fatalf("container could access an unrelated host path: %v", runErr)
	}
	variant.Args = []string{"-c", "touch /workspace/forbidden"}
	invocation, err = DockerInvocation(readiness.Runtime, readiness.Version, variant, DockerPolicy{Workspace: workspace, Permission: spec.PermissionReadonly, Network: false, Name: "agentworks-policy-write-test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, runErr := awprocess.Run(ctx, invocation.Process); runErr == nil {
		t.Fatal("readonly container unexpectedly wrote its workspace")
	}
	if _, err := os.Stat(filepath.Join(workspace, "forbidden")); !os.IsNotExist(err) {
		t.Fatalf("readonly container created workspace file: %v", err)
	}
}

func TestRelativeHostExecutableMustBeRunnable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	variant := spec.RuntimeVariant{Provider: spec.ProviderHost, Command: spec.Command{"./tool"}}
	if got := (Probe{}).CheckHost(context.Background(), variant, dir); got.State != StateMissing {
		t.Fatalf("non-executable=%#v", got)
	}
}
