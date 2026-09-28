// Package connectors polls external systems into durable AgentWorks events and outcomes.
package connectors

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/monitors"
	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
)

type Emission struct {
	Envelope router.Envelope
	WorkItem store.WorkItem
}

type Batch struct {
	Cursor           string
	Events           []Emission
	WorkItems        []store.WorkItem
	Outcomes         []store.Outcome
	RateLimitResetAt time.Time
}

type Connector interface {
	Test(context.Context) error
	Poll(context.Context, string, time.Time) (Batch, error)
}

type Factory func(spec.Source) (Connector, error)

type RateLimitError struct {
	ResetAt time.Time
}

func (err *RateLimitError) Error() string { return "connector rate limit reached" }

type Service struct {
	Root    string
	Store   *store.Store
	Router  *router.Service
	Factory Factory
	Now     func() time.Time
}

type PollResult struct {
	Source  store.SourceStatus      `json:"source"`
	Commit  store.PollCommitResult  `json:"commit"`
	Routed  int                     `json:"routed"`
	Attempt store.SourcePollAttempt `json:"attempt"`
}

func (service *Service) Sync(ctx context.Context) ([]spec.DefinitionIssue, error) {
	sources, issues := spec.DiscoverSources(service.Root)
	now := service.now()
	active := map[string]bool{}
	for _, source := range sources {
		if source.Kind != spec.SourceJira && source.Kind != spec.SourceGitHub {
			continue
		}
		active[source.Name] = true
		if err := service.Store.SyncSource(ctx, store.SourceStatus{ID: source.Name, Kind: string(source.Kind), ConfigRevision: revision(source), DefinitionEnabled: source.IsEnabled(), CreatedAt: now, UpdatedAt: now, NextPollAt: now}); err != nil {
			return issues, err
		}
	}
	return issues, service.Store.DisableConnectorSourcesExcept(ctx, active, now)
}

func (service *Service) PollNow(ctx context.Context, name string) (PollResult, error) {
	source, err := spec.LoadSource(service.Root, name)
	if err != nil {
		return PollResult{}, err
	}
	if source.Kind != spec.SourceJira && source.Kind != spec.SourceGitHub {
		return PollResult{}, fmt.Errorf("source %q is not an external connector", name)
	}
	if _, err := service.Sync(ctx); err != nil {
		return PollResult{}, err
	}
	status, err := service.Store.Source(ctx, name)
	if err != nil {
		return PollResult{}, err
	}
	if !status.Enabled() {
		return PollResult{}, errors.New("connector is paused or disabled")
	}
	started := service.now()
	attempt := store.SourcePollAttempt{ID: newPollAttemptID(), SourceID: name, State: "running", CursorBefore: status.Cursor, StartedAt: started}
	if err := service.Store.StartSourcePoll(ctx, attempt); err != nil {
		return PollResult{}, err
	}
	factory := service.Factory
	if factory == nil {
		factory = service.defaultFactory
	}
	connector, err := factory(*source)
	if err != nil {
		return PollResult{}, service.fail(ctx, status, *source, attempt, err)
	}
	now := service.now()
	batch, err := connector.Poll(ctx, status.Cursor, now)
	if err != nil {
		return PollResult{}, service.fail(ctx, status, *source, attempt, err)
	}
	commit := store.PollCommit{SourceID: name, AttemptID: attempt.ID, ExpectedCursor: status.Cursor, Cursor: batch.Cursor, WorkItems: append([]store.WorkItem{}, batch.WorkItems...), Outcomes: batch.Outcomes, PolledAt: now, RateLimitResetAt: batch.RateLimitResetAt}
	interval, _ := source.Interval()
	commit.NextPollAt = now.Add(interval)
	for _, emission := range batch.Events {
		if emission.Envelope.Source != name {
			return PollResult{}, service.fail(ctx, status, *source, attempt, errors.New("connector emitted an event for another source"))
		}
		event, err := router.Normalize(emission.Envelope, now)
		if err != nil {
			return PollResult{}, service.fail(ctx, status, *source, attempt, err)
		}
		if emission.WorkItem.ID != "" {
			event.WorkItemID = emission.WorkItem.ID
			commit.WorkItems = append(commit.WorkItems, emission.WorkItem)
		}
		commit.Events = append(commit.Events, event)
	}
	commit.WorkItems = uniqueWorkItems(commit.WorkItems)
	stored, err := service.Store.CommitPoll(ctx, commit)
	if err != nil {
		return PollResult{}, service.fail(ctx, status, *source, attempt, err)
	}
	routed := 0
	runIDs := []string{}
	var routeErrors []error
	for _, eventID := range stored.EventRecordIDs {
		result, err := service.Router.DispatchStored(ctx, eventID)
		if err != nil {
			routeErrors = append(routeErrors, err)
			continue
		}
		if result.Run != nil {
			routed++
			runIDs = append(runIDs, result.Run.ID)
		}
	}
	completed, completionErr := service.Store.CompleteSourcePoll(ctx, store.SourcePollAttempt{ID: attempt.ID, RoutedRuns: routed, RunIDs: runIDs, FinishedAt: service.now()})
	if completionErr != nil {
		return PollResult{}, completionErr
	}
	_ = monitors.EvaluateAndStore(ctx, service.Store, now)
	updated, _ := service.Store.Source(ctx, name)
	return PollResult{Source: updated, Commit: stored, Routed: routed, Attempt: completed}, errors.Join(routeErrors...)
}

func (service *Service) Test(ctx context.Context, name string) error {
	source, err := spec.LoadSource(service.Root, name)
	if err != nil {
		return err
	}
	if _, err = service.Sync(ctx); err != nil {
		return err
	}
	status, err := service.Store.Source(ctx, name)
	if err != nil {
		return err
	}
	factory := service.Factory
	if factory == nil {
		factory = service.defaultFactory
	}
	connector, err := factory(*source)
	if err != nil {
		return service.failTest(ctx, status, *source, err)
	}
	if err = connector.Test(ctx); err != nil {
		return service.failTest(ctx, status, *source, err)
	}
	return service.Store.RecordSourceTest(ctx, name, service.now())
}

func (service *Service) SetPaused(ctx context.Context, name string, paused bool) error {
	if _, err := spec.LoadSource(service.Root, name); err != nil {
		return err
	}
	if _, err := service.Sync(ctx); err != nil {
		return err
	}
	return service.Store.SetSourcePaused(ctx, name, paused, service.now())
}

func (service *Service) Definitions(ctx context.Context) ([]spec.Source, []spec.DefinitionIssue, []store.SourceStatus, error) {
	_, err := service.Sync(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	sources, issues := spec.DiscoverSources(service.Root)
	statuses, err := service.Store.ListSources(ctx)
	return sources, issues, statuses, err
}

func (service *Service) fail(ctx context.Context, status store.SourceStatus, source spec.Source, attempt store.SourcePollAttempt, cause error) error {
	now := service.now()
	interval, _ := source.Interval()
	backoff := interval
	for count := 0; count < status.FailureCount && backoff < 15*time.Minute; count++ {
		backoff *= 2
	}
	if backoff > 15*time.Minute {
		backoff = 15 * time.Minute
	}
	next := now.Add(backoff)
	var limited *RateLimitError
	var reset time.Time
	if errors.As(cause, &limited) && limited.ResetAt.After(next) {
		next, reset = limited.ResetAt, limited.ResetAt
	}
	attempt.FinishedAt, attempt.NextPollAt, attempt.RateLimitResetAt = now, next, reset
	attempt.ErrorCode, attempt.ErrorMessage = diagnosticErrorCode(cause), cause.Error()
	if _, err := service.Store.FailSourcePoll(ctx, attempt); err != nil {
		return fmt.Errorf("record connector failure: %w", err)
	}
	return cause
}

func (service *Service) failTest(ctx context.Context, status store.SourceStatus, source spec.Source, cause error) error {
	now := service.now()
	interval, _ := source.Interval()
	backoff := interval
	for count := 0; count < status.FailureCount && backoff < 15*time.Minute; count++ {
		backoff *= 2
	}
	if backoff > 15*time.Minute {
		backoff = 15 * time.Minute
	}
	next := now.Add(backoff)
	var limited *RateLimitError
	var reset time.Time
	if errors.As(cause, &limited) && limited.ResetAt.After(next) {
		next, reset = limited.ResetAt, limited.ResetAt
	}
	_ = service.Store.RecordSourceFailure(ctx, source.Name, cause.Error(), next, reset, now)
	return cause
}

func diagnosticErrorCode(cause error) string {
	var limited *RateLimitError
	if errors.As(cause, &limited) {
		return "rate_limited"
	}
	var response *HTTPError
	if errors.As(cause, &response) {
		return fmt.Sprintf("http_%d", response.Status)
	}
	message := strings.ToLower(cause.Error())
	switch {
	case strings.Contains(message, "credentials are unavailable"):
		return "credentials_unavailable"
	case strings.Contains(message, "decode connector response"):
		return "invalid_response"
	case strings.Contains(message, "redirect"):
		return "unsafe_redirect"
	default:
		return "connector_failure"
	}
}

func newPollAttemptID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return "poll_" + hex.EncodeToString(bytes)
	}
	return stableID("poll", fmt.Sprint(time.Now().UTC().UnixNano()))
}

func (service *Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func (service *Service) defaultFactory(source spec.Source) (Connector, error) {
	switch source.Kind {
	case spec.SourceJira:
		email, emailOK := os.LookupEnv(source.Jira.EmailEnv)
		token, tokenOK := os.LookupEnv(source.Jira.TokenEnv)
		if !emailOK || strings.TrimSpace(email) == "" || !tokenOK || strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("Jira credentials are unavailable; set %s and %s", source.Jira.EmailEnv, source.Jira.TokenEnv)
		}
		return NewJira(source, email, token, nil)
	case spec.SourceGitHub:
		token, ok := os.LookupEnv(source.GitHub.TokenEnv)
		if !ok || strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("GitHub credentials are unavailable; set %s", source.GitHub.TokenEnv)
		}
		return NewGitHub(source, token, nil)
	default:
		return nil, fmt.Errorf("unsupported connector kind %q", source.Kind)
	}
}

func uniqueWorkItems(values []store.WorkItem) []store.WorkItem {
	byID := map[string]store.WorkItem{}
	for _, value := range values {
		if value.ID != "" {
			byID[value.ID] = value
		}
	}
	keys := make([]string, 0, len(byID))
	for key := range byID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]store.WorkItem, 0, len(keys))
	for _, key := range keys {
		result = append(result, byID[key])
	}
	return result
}

func revision(source spec.Source) string {
	data, _ := json.Marshal(source)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func stableID(prefix string, parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(digest[:16])
}
