package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
)

func TestInspectTeamFallsBackToContainer(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	probe := Probe{
		LookPath: func(name string) (string, error) {
			if name == "docker" {
				return "/usr/bin/docker", nil
			}
			return "", errors.New("missing")
		},
		VersionOutput: func(context.Context, string) ([]byte, error) { return []byte("28.0.0"), nil },
	}
	results, err := InspectTeam(context.Background(), root, "engineering", probe)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Selected == nil || results[0].Selected.Provider != "container" || results[0].Variants[0].State != StateMissing {
		t.Fatalf("results=%#v", results)
	}
}

func TestReadySelectorRecordsContainerRuntime(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	probe := Probe{
		LookPath: func(name string) (string, error) {
			if name == "docker" {
				return "/usr/bin/docker", nil
			}
			return "", errors.New("missing")
		},
		VersionOutput: func(context.Context, string) ([]byte, error) { return []byte("28.0.0"), nil },
	}
	plan, err := resolver.ResolveTeam(root, "engineering", resolver.Options{
		AvailableProviders: []spec.Provider{spec.ProviderHost, spec.ProviderContainer},
		SelectVariant:      ReadySelector(context.Background(), probe),
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := plan.Tools[0]
	if tool.SelectedProvider != spec.ProviderContainer || tool.Runtime != "/usr/bin/docker" || tool.RuntimeVersion != "28.0.0" || tool.ImageDigest == "" || tool.Network != "policy-controlled" {
		t.Fatalf("tool plan=%#v", tool)
	}
}

func TestInspectTeamReportsNoReadyRuntime(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	probe := Probe{LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	results, err := InspectTeam(context.Background(), root, "engineering", probe)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Selected != nil || results[0].Error == "" {
		t.Fatalf("results=%#v", results)
	}
}
