package providers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
)

// ReadySelector returns a resolver callback that chooses the first declared,
// allowed variant that is actually usable on this machine.
func ReadySelector(ctx context.Context, probe Probe) resolver.VariantSelector {
	return func(toolName, workdir string, variants []spec.RuntimeVariant, available []spec.Provider) (resolver.VariantSelection, error) {
		allowed := map[spec.Provider]bool{}
		for _, provider := range available {
			if !provider.Valid() {
				return resolver.VariantSelection{}, fmt.Errorf("available provider %q is invalid", provider)
			}
			allowed[provider] = true
		}
		var diagnostics []string
		for _, variant := range variants {
			if !allowed[variant.Provider] {
				continue
			}
			readiness := checkVariant(ctx, probe, variant, workdir)
			if readiness.State == StateReady {
				return resolver.VariantSelection{
					Variant: variant, Runtime: readiness.Runtime, RuntimeVersion: readiness.Version,
					ImageDigest: readiness.ImageDigest, Network: readiness.Network,
				}, nil
			}
			diagnostics = append(diagnostics, fmt.Sprintf("%s: %s", variant.Provider, readiness.Detail))
		}
		if len(diagnostics) == 0 {
			return resolver.VariantSelection{}, errors.New("no compatible runtime variant")
		}
		return resolver.VariantSelection{}, fmt.Errorf("no ready runtime variant (%s)", strings.Join(diagnostics, "; "))
	}
}

func checkVariant(ctx context.Context, probe Probe, variant spec.RuntimeVariant, workdir string) Readiness {
	switch variant.Provider {
	case spec.ProviderHost:
		return probe.CheckHost(ctx, variant, workdir)
	case spec.ProviderContainer:
		return probe.CheckDocker(ctx, variant)
	case spec.ProviderRemote:
		return Readiness{Provider: spec.ProviderRemote, State: StateReady, Runtime: variant.URL, Network: "required"}
	default:
		return Readiness{Provider: variant.Provider, State: StateIncompatible, Network: "unknown", Detail: "provider is unsupported"}
	}
}

type ToolReadiness struct {
	Name     string      `json:"name"`
	Selected *Readiness  `json:"selected,omitempty"`
	Variants []Readiness `json:"variants"`
	Error    string      `json:"error,omitempty"`
}

// InspectTeam checks every provider variant in declared preference order and
// selects the first ready option without starting or pulling a tool.
func InspectTeam(ctx context.Context, root, teamName string, probe Probe) ([]ToolReadiness, error) {
	plan, err := resolver.ResolveTeam(root, teamName, resolver.Options{AvailableProviders: []spec.Provider{
		spec.ProviderHost, spec.ProviderContainer, spec.ProviderRemote,
	}})
	if err != nil {
		return nil, err
	}
	results := make([]ToolReadiness, 0, len(plan.Tools))
	for _, planned := range plan.Tools {
		var tool *spec.Tool
		if planned.Origin == resolver.OriginInstalled {
			tool, err = spec.LoadInstalledTool(root, planned.Name)
		} else {
			tool, err = spec.LoadTool(root, planned.Name)
		}
		if err != nil {
			return nil, err
		}
		workdir := filepath.Join(root, "tools", filepath.FromSlash(planned.Name))
		if planned.Origin == resolver.OriginInstalled {
			workdir = filepath.Join(root, ".agentworks", "deps", "tools", filepath.FromSlash(planned.Name))
		}
		result := ToolReadiness{Name: planned.Name, Variants: []Readiness{}}
		for _, variant := range tool.Runtime.Variants {
			readiness := checkVariant(ctx, probe, variant, workdir)
			result.Variants = append(result.Variants, readiness)
			if result.Selected == nil && readiness.State == StateReady {
				copy := readiness
				result.Selected = &copy
			}
		}
		if result.Selected == nil {
			result.Error = fmt.Sprintf("tool %q has no ready runtime; inspect variant diagnostics", planned.Name)
		}
		results = append(results, result)
	}
	return results, nil
}
