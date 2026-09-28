package spec

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var namePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
var pinnedImagePattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)
var windowsAbsolutePattern = regexp.MustCompile(`^[A-Za-z]:/`)

func (project Project) Validate() error {
	var errs []error
	if project.Format != CurrentFormat {
		errs = append(errs, fmt.Errorf("format must be %d, got %d", CurrentFormat, project.Format))
	}
	if err := validateName("project name", project.Name); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, validateRefs("teams", project.Teams)...)
	errs = append(errs, validateRefs("workspaces", project.Workspaces)...)
	errs = append(errs, validateDependencies("dependencies.skills", project.Dependencies.Skills)...)
	errs = append(errs, validateDependencies("dependencies.tools", project.Dependencies.Tools)...)
	if _, err := ParseByteSize(project.Runtime.StorageLimit); err != nil {
		errs = append(errs, fmt.Errorf("runtime.storage_limit: %w", err))
	}
	if duration, err := time.ParseDuration(project.Runtime.ShutdownGrace); err != nil || duration <= 0 {
		if err != nil {
			errs = append(errs, fmt.Errorf("runtime.shutdown_grace: %w", err))
		} else {
			errs = append(errs, errors.New("runtime.shutdown_grace must be greater than zero"))
		}
	}
	return errors.Join(errs...)
}

func (local LocalConfig) Validate() error {
	var errs []error
	for alias, binding := range local.Workspaces {
		if err := validateRef("workspace alias", alias); err != nil {
			errs = append(errs, err)
		}
		if binding.Path == "" {
			errs = append(errs, fmt.Errorf("workspace %q path is required", alias))
		} else if !filepath.IsAbs(binding.Path) {
			errs = append(errs, fmt.Errorf("workspace %q path must be absolute", alias))
		}
	}
	return errors.Join(errs...)
}

func (team Team) Validate() error {
	var errs []error
	if err := validateName("team name", team.Name); err != nil {
		errs = append(errs, err)
	}
	if len(team.Agents) == 0 {
		errs = append(errs, errors.New("agents must contain at least one agent"))
	}
	errs = append(errs, validateRefs("agents", team.Agents)...)
	if team.DefaultAgent != "" {
		if err := validateRef("default_agent", team.DefaultAgent); err != nil {
			errs = append(errs, err)
		}
		if !contains(team.Agents, team.DefaultAgent) {
			errs = append(errs, fmt.Errorf("default_agent %q is not listed in agents", team.DefaultAgent))
		}
	}
	if err := validatePortableRelativePath("memory.team", team.Memory.Team); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (agent Agent) Validate() error {
	var errs []error
	if err := validateName("agent name", agent.Name); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, validateRefs("skills", agent.Skills)...)
	errs = append(errs, validateRefs("tools", agent.Tools)...)
	errs = append(errs, validateRefs("delegates", agent.Delegates)...)
	if !agent.MaxPermission.Valid() {
		errs = append(errs, fmt.Errorf("max_permission %q is invalid", agent.MaxPermission))
	}
	if err := validatePortableRelativePath("memory", agent.Memory); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(agent.Instructions) == "" {
		errs = append(errs, errors.New("agent instructions must not be empty"))
	}
	return errors.Join(errs...)
}

func (tool Tool) Validate() error {
	var errs []error
	if err := validateName("tool name", tool.Name); err != nil {
		errs = append(errs, err)
	}
	if !tool.Transport.Valid() {
		errs = append(errs, fmt.Errorf("transport %q is invalid", tool.Transport))
	}
	if len(tool.Runtime.Variants) == 0 {
		errs = append(errs, errors.New("runtime.variants must contain at least one variant"))
	}
	seen := map[Provider]struct{}{}
	for index, variant := range tool.Runtime.Variants {
		field := fmt.Sprintf("runtime.variants[%d]", index)
		if !variant.Provider.Valid() {
			errs = append(errs, fmt.Errorf("%s provider %q is invalid", field, variant.Provider))
			continue
		}
		if _, exists := seen[variant.Provider]; exists {
			errs = append(errs, fmt.Errorf("runtime.variants contains duplicate provider %q", variant.Provider))
		}
		seen[variant.Provider] = struct{}{}
		switch variant.Provider {
		case ProviderHost:
			if tool.Transport != TransportStdio {
				errs = append(errs, fmt.Errorf("%s host provider requires stdio transport", field))
			}
			if len(variant.Command) == 0 {
				errs = append(errs, fmt.Errorf("%s command is required for host provider", field))
			}
			if variant.Image != "" || variant.URL != "" {
				errs = append(errs, fmt.Errorf("%s host provider cannot set image or url", field))
			}
		case ProviderContainer:
			if tool.Transport != TransportStdio {
				errs = append(errs, fmt.Errorf("%s container provider requires stdio transport", field))
			}
			if len(variant.Command) == 0 {
				errs = append(errs, fmt.Errorf("%s command is required for container provider", field))
			}
			if !pinnedImagePattern.MatchString(variant.Image) {
				errs = append(errs, fmt.Errorf("%s image must use a pinned sha256 digest", field))
			}
			if variant.URL != "" {
				errs = append(errs, fmt.Errorf("%s container provider cannot set url", field))
			}
		case ProviderRemote:
			if tool.Transport == TransportStdio {
				errs = append(errs, fmt.Errorf("%s remote provider requires http or sse transport", field))
			}
			parsed, err := url.ParseRequestURI(variant.URL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				errs = append(errs, fmt.Errorf("%s url must be an absolute http or https URL", field))
			}
			if len(variant.Command) > 0 || variant.Image != "" {
				errs = append(errs, fmt.Errorf("%s remote provider cannot set command or image", field))
			}
		}
	}
	errs = append(errs, validateEnvNames("auth", tool.Auth)...)
	return errors.Join(errs...)
}

func (transport Transport) Valid() bool {
	switch transport {
	case TransportStdio, TransportHTTP, TransportSSE:
		return true
	default:
		return false
	}
}

func (provider Provider) Valid() bool {
	switch provider {
	case ProviderHost, ProviderContainer, ProviderRemote:
		return true
	default:
		return false
	}
}

func (permission Permission) Valid() bool {
	_, ok := permissionRank(permission)
	return ok
}

// EffectivePermission returns the least-authoritative permission in the set.
// This enforces the rule that harness and route policy may reduce, but never
// increase, an agent's declared maximum permission.
func EffectivePermission(permissions ...Permission) (Permission, error) {
	if len(permissions) == 0 {
		return "", errors.New("at least one permission is required")
	}
	effective := PermissionAutonomous
	effectiveRank, _ := permissionRank(effective)
	for _, permission := range permissions {
		rank, ok := permissionRank(permission)
		if !ok {
			return "", fmt.Errorf("permission %q is invalid", permission)
		}
		if rank < effectiveRank {
			effective = permission
			effectiveRank = rank
		}
	}
	return effective, nil
}

func permissionRank(permission Permission) (int, bool) {
	switch permission {
	case PermissionReadonly:
		return 0, true
	case PermissionReadwrite:
		return 1, true
	case PermissionCollaborate:
		return 2, true
	case PermissionAutonomous:
		return 3, true
	default:
		return 0, false
	}
}

// ParseByteSize parses the deliberately small storage-limit vocabulary used by
// the local runtime. Decimal units make the configured 1GB limit unambiguous.
func ParseByteSize(value string) (int64, error) {
	upper := strings.ToUpper(strings.TrimSpace(value))
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"GB", 1_000_000_000},
		{"MB", 1_000_000},
		{"KB", 1_000},
		{"B", 1},
	}
	for _, unit := range units {
		if !strings.HasSuffix(upper, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(upper, unit.suffix))
		amount, err := strconv.ParseInt(number, 10, 64)
		if err != nil || amount <= 0 {
			return 0, fmt.Errorf("must be a positive integer followed by B, KB, MB, or GB")
		}
		if amount > math.MaxInt64/unit.multiplier {
			return 0, errors.New("storage limit is too large")
		}
		return amount * unit.multiplier, nil
	}
	return 0, errors.New("must be a positive integer followed by B, KB, MB, or GB")
}

func validateName(field, value string) error {
	if !namePattern.MatchString(value) {
		return fmt.Errorf("%s %q must be a lowercase kebab-case name", field, value)
	}
	return nil
}

func validateRef(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	for _, part := range strings.Split(value, "/") {
		if err := validateName(field, part); err != nil {
			return err
		}
	}
	return nil
}

func validateRefs(field string, values []string) []error {
	var errs []error
	seen := map[string]struct{}{}
	for _, value := range values {
		if err := validateRef(field, value); err != nil {
			errs = append(errs, err)
		}
		if _, exists := seen[value]; exists {
			errs = append(errs, fmt.Errorf("%s contains duplicate %q", field, value))
		}
		seen[value] = struct{}{}
	}
	return errs
}

func validateDependencies(field string, dependencies map[string]Dependency) []error {
	var errs []error
	for name, dependency := range dependencies {
		if err := validateRef(field, name); err != nil {
			errs = append(errs, err)
		}
		if strings.TrimSpace(dependency.Source) == "" {
			errs = append(errs, fmt.Errorf("%s %q source is required", field, name))
		}
		if err := validatePortableRelativePath(fmt.Sprintf("%s %q path", field, name), dependency.Path); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func validatePortableRelativePath(field, value string) error {
	if value == "" {
		return nil
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.ContainsRune(normalized, '\x00') || pathpkg.IsAbs(normalized) || windowsAbsolutePattern.MatchString(normalized) {
		return fmt.Errorf("%s must be project-relative", field)
	}
	clean := pathpkg.Clean(normalized)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%s must not escape its root", field)
	}
	return nil
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateEnvNames(field string, values []string) []error {
	var errs []error
	seen := map[string]struct{}{}
	for _, value := range values {
		if !envNamePattern.MatchString(value) {
			errs = append(errs, fmt.Errorf("%s value %q is not a valid environment variable name", field, value))
		}
		if _, exists := seen[value]; exists {
			errs = append(errs, fmt.Errorf("%s contains duplicate %q", field, value))
		}
		seen[value] = struct{}{}
	}
	return errs
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
