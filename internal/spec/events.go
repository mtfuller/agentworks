package spec

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var githubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type SourceKind string

const (
	SourceManual    SourceKind = "manual"
	SourceScheduled SourceKind = "schedule"
	SourceJira      SourceKind = "jira"
	SourceGitHub    SourceKind = "github"
)

type Source struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Kind        SourceKind     `yaml:"kind" json:"kind"`
	Enabled     *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Schedule    SourceSchedule `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	Poll        SourcePoll     `yaml:"poll,omitempty" json:"poll,omitempty"`
	Event       SourceEvent    `yaml:"event,omitempty" json:"event,omitempty"`
	Jira        JiraSource     `yaml:"jira,omitempty" json:"jira,omitempty"`
	GitHub      GitHubSource   `yaml:"github,omitempty" json:"github,omitempty"`
	Path        string         `yaml:"-" json:"path,omitempty"`
}

type SourcePoll struct {
	Every           string `yaml:"every,omitempty" json:"every,omitempty"`
	InitialLookback string `yaml:"initial_lookback,omitempty" json:"initial_lookback,omitempty"`
}

type JiraSource struct {
	BaseURL  string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	JQL      string `yaml:"jql,omitempty" json:"jql,omitempty"`
	EmailEnv string `yaml:"email_env,omitempty" json:"email_env,omitempty"`
	TokenEnv string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
}

type GitHubSource struct {
	APIURL          string `yaml:"api_url,omitempty" json:"api_url,omitempty"`
	Repository      string `yaml:"repository,omitempty" json:"repository,omitempty"`
	TokenEnv        string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
	WorkItemPattern string `yaml:"work_item_pattern,omitempty" json:"work_item_pattern,omitempty"`
}

type SourceSchedule struct {
	Every   string    `yaml:"every,omitempty" json:"every,omitempty"`
	StartAt time.Time `yaml:"start_at,omitempty" json:"start_at,omitempty"`
	CatchUp string    `yaml:"catch_up,omitempty" json:"catch_up,omitempty"`
}

type SourceEvent struct {
	Type    string         `yaml:"type,omitempty" json:"type,omitempty"`
	Subject string         `yaml:"subject,omitempty" json:"subject,omitempty"`
	Data    map[string]any `yaml:"data,omitempty" json:"data,omitempty"`
}

func (source Source) IsEnabled() bool { return source.Enabled == nil || *source.Enabled }

func (source Source) Interval() (time.Duration, error) {
	if source.Kind == SourceJira || source.Kind == SourceGitHub {
		return time.ParseDuration(source.Poll.Every)
	}
	return time.ParseDuration(source.Schedule.Every)
}

func (source Source) Validate() error {
	var errs []error
	if err := validateName("source name", source.Name); err != nil {
		errs = append(errs, err)
	}
	if source.Kind != SourceManual && source.Kind != SourceScheduled && source.Kind != SourceJira && source.Kind != SourceGitHub {
		errs = append(errs, fmt.Errorf("source kind %q is invalid", source.Kind))
	}
	if source.Kind == SourceScheduled {
		interval, err := source.Interval()
		if err != nil || interval <= 0 {
			errs = append(errs, errors.New("schedule.every must be a positive duration"))
		}
		if source.Schedule.CatchUp == "" {
			source.Schedule.CatchUp = "latest"
		}
		if source.Schedule.CatchUp != "all" && source.Schedule.CatchUp != "latest" && source.Schedule.CatchUp != "skip" {
			errs = append(errs, errors.New("schedule.catch_up must be all, latest, or skip"))
		}
		if strings.TrimSpace(source.Event.Type) == "" {
			errs = append(errs, errors.New("event.type is required for a schedule source"))
		}
	}
	if source.Kind == SourceJira || source.Kind == SourceGitHub {
		interval, err := source.Interval()
		if err != nil || interval < 10*time.Second {
			errs = append(errs, errors.New("poll.every must be a duration of at least 10s"))
		}
		if source.Poll.InitialLookback != "" {
			lookback, err := time.ParseDuration(source.Poll.InitialLookback)
			if err != nil || lookback <= 0 || lookback > 30*24*time.Hour {
				errs = append(errs, errors.New("poll.initial_lookback must be a positive duration no greater than 720h"))
			}
		}
	}
	if source.Kind == SourceJira {
		if err := validateConnectorURL("jira.base_url", source.Jira.BaseURL); err != nil {
			errs = append(errs, err)
		}
		if strings.TrimSpace(source.Jira.JQL) == "" {
			errs = append(errs, errors.New("jira.jql is required"))
		}
		if !envNamePattern.MatchString(source.Jira.EmailEnv) || !envNamePattern.MatchString(source.Jira.TokenEnv) {
			errs = append(errs, errors.New("jira email_env and token_env must be environment variable names"))
		}
	}
	if source.Kind == SourceGitHub {
		if source.GitHub.APIURL != "" {
			if err := validateConnectorURL("github.api_url", source.GitHub.APIURL); err != nil {
				errs = append(errs, err)
			}
		}
		if !githubRepositoryPattern.MatchString(source.GitHub.Repository) {
			errs = append(errs, errors.New("github.repository must be owner/name"))
		}
		if !envNamePattern.MatchString(source.GitHub.TokenEnv) {
			errs = append(errs, errors.New("github.token_env must be an environment variable name"))
		}
		if source.GitHub.WorkItemPattern != "" {
			if _, err := regexp.Compile(source.GitHub.WorkItemPattern); err != nil {
				errs = append(errs, errors.New("github.work_item_pattern must be a valid regular expression"))
			}
		}
	}
	return errors.Join(errs...)
}

func validateConnectorURL(field, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an http(s) origin without credentials, query, or fragment", field)
	}
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("%s must use https unless it is loopback", field)
		}
	}
	return nil
}

type Route struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Priority    int         `yaml:"priority,omitempty" json:"priority"`
	Enabled     *bool       `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	When        RouteWhen   `yaml:"when" json:"when"`
	Invoke      RouteInvoke `yaml:"invoke" json:"invoke"`
	Tests       []RouteTest `yaml:"tests,omitempty" json:"tests,omitempty"`
	Path        string      `yaml:"-" json:"path,omitempty"`
}

// RouteTest is a persisted preview fixture. It exercises the same global
// evaluator as ingestion but never creates an event or run.
type RouteTest struct {
	Name   string         `yaml:"name" json:"name"`
	Event  RouteTestEvent `yaml:"event" json:"event"`
	Expect string         `yaml:"expect" json:"expect"`
}

type RouteTestEvent struct {
	Source  string         `yaml:"source" json:"source"`
	Type    string         `yaml:"type" json:"type"`
	Subject string         `yaml:"subject,omitempty" json:"subject,omitempty"`
	Data    map[string]any `yaml:"data,omitempty" json:"data,omitempty"`
}

type RouteWhen struct {
	Source string         `yaml:"source,omitempty" json:"source,omitempty"`
	Type   string         `yaml:"type,omitempty" json:"type,omitempty"`
	Fields map[string]any `yaml:"fields,omitempty" json:"fields,omitempty"`
}
type RouteInvoke struct {
	Team       string     `yaml:"team" json:"team"`
	Agent      string     `yaml:"agent,omitempty" json:"agent,omitempty"`
	Workspace  string     `yaml:"workspace" json:"workspace"`
	Harness    string     `yaml:"harness,omitempty" json:"harness,omitempty"`
	Permission Permission `yaml:"permission,omitempty" json:"permission,omitempty"`
}

func (route Route) IsEnabled() bool { return route.Enabled == nil || *route.Enabled }

func (route Route) Validate() error {
	var errs []error
	if err := validateName("route name", route.Name); err != nil {
		errs = append(errs, err)
	}
	if route.When.Source == "" && route.When.Type == "" && len(route.When.Fields) == 0 {
		errs = append(errs, errors.New("when must contain source, type, or fields"))
	}
	if route.Invoke.Team == "" {
		errs = append(errs, errors.New("invoke.team is required"))
	} else if err := validateRef("invoke.team", route.Invoke.Team); err != nil {
		errs = append(errs, err)
	}
	if route.Invoke.Agent != "" {
		if err := validateRef("invoke.agent", route.Invoke.Agent); err != nil {
			errs = append(errs, err)
		}
	}
	if err := validateRef("invoke.workspace", route.Invoke.Workspace); err != nil {
		errs = append(errs, err)
	}
	if route.Invoke.Harness != "" {
		if err := validateName("invoke.harness", route.Invoke.Harness); err != nil {
			errs = append(errs, err)
		}
	}
	if route.Invoke.Permission != "" && !route.Invoke.Permission.Valid() {
		errs = append(errs, fmt.Errorf("invoke.permission %q is invalid", route.Invoke.Permission))
	}
	for field, value := range route.When.Fields {
		if strings.TrimSpace(field) == "" || strings.Contains(field, "/") {
			errs = append(errs, fmt.Errorf("when.fields key %q is invalid", field))
		}
		switch value.(type) {
		case nil, string, bool, int, int64, float64:
		default:
			errs = append(errs, fmt.Errorf("when.fields %q must be a scalar", field))
		}
	}
	testNames := map[string]bool{}
	for index, test := range route.Tests {
		if strings.TrimSpace(test.Name) == "" {
			errs = append(errs, fmt.Errorf("tests[%d].name is required", index))
		} else if testNames[test.Name] {
			errs = append(errs, fmt.Errorf("tests[%d].name %q is duplicated", index, test.Name))
		}
		testNames[test.Name] = true
		if strings.TrimSpace(test.Event.Source) == "" || strings.TrimSpace(test.Event.Type) == "" {
			errs = append(errs, fmt.Errorf("tests[%d].event source and type are required", index))
		}
		if strings.TrimSpace(test.Expect) == "" {
			errs = append(errs, fmt.Errorf("tests[%d].expect is required", index))
		}
	}
	return errors.Join(errs...)
}

type DefinitionIssue struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func LoadSource(root, name string) (*Source, error) {
	if err := validateRef("source reference", name); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "sources", filepath.FromSlash(name), SourceFile)
	var value Source
	if err := decodeFile(path, &value); err != nil {
		return nil, err
	}
	value.Path = filepath.ToSlash(filepath.Join("sources", filepath.FromSlash(name), SourceFile))
	if value.Schedule.CatchUp == "" {
		value.Schedule.CatchUp = "latest"
	}
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if value.Name != pathBase(name) {
		return nil, fmt.Errorf("%s: source name %q does not match directory %q", path, value.Name, pathBase(name))
	}
	return &value, nil
}

func LoadRoute(root, name string) (*Route, error) {
	if err := validateRef("route reference", name); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "routes", filepath.FromSlash(name), RouteFile)
	var value Route
	if err := decodeFile(path, &value); err != nil {
		return nil, err
	}
	value.Path = filepath.ToSlash(filepath.Join("routes", filepath.FromSlash(name), RouteFile))
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if value.Name != pathBase(name) {
		return nil, fmt.Errorf("%s: route name %q does not match directory %q", path, value.Name, pathBase(name))
	}
	return &value, nil
}

func DiscoverSources(root string) ([]Source, []DefinitionIssue) {
	values := []Source{}
	issues := []DefinitionIssue{}
	discoverDefinitions(root, "sources", SourceFile, func(ref, path string) {
		value, err := LoadSource(root, ref)
		if err != nil {
			issues = append(issues, DefinitionIssue{Path: path, Error: err.Error()})
			return
		}
		values = append(values, *value)
	})
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, issues
}
func DiscoverRoutes(root string) ([]Route, []DefinitionIssue) {
	values := []Route{}
	issues := []DefinitionIssue{}
	discoverDefinitions(root, "routes", RouteFile, func(ref, path string) {
		value, err := LoadRoute(root, ref)
		if err != nil {
			issues = append(issues, DefinitionIssue{Path: path, Error: err.Error()})
			return
		}
		values = append(values, *value)
	})
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, issues
}

func discoverDefinitions(root, dir, file string, visit func(string, string)) {
	base := filepath.Join(root, dir)
	_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() != file {
			return nil
		}
		relative, relErr := filepath.Rel(base, filepath.Dir(path))
		if relErr == nil {
			visit(filepath.ToSlash(relative), filepath.ToSlash(path))
		}
		return nil
	})
}
