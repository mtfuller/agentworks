// Package router normalizes events and deterministically selects exactly one route.
package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
)

var envelopeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

type Envelope struct {
	SpecVersion string          `json:"specversion,omitempty"`
	ID          string          `json:"id"`
	Source      string          `json:"source"`
	Type        string          `json:"type"`
	Subject     string          `json:"subject,omitempty"`
	Time        time.Time       `json:"time"`
	Data        json.RawMessage `json:"data"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
}

type Match struct {
	Route    string `json:"route"`
	Priority int    `json:"priority"`
}
type Decision struct {
	Matches  []Match                `json:"matches"`
	Selected string                 `json:"selected,omitempty"`
	Reason   string                 `json:"reason"`
	Issues   []spec.DefinitionIssue `json:"issues"`
}
type Result struct {
	Event    store.Event `json:"event"`
	Decision Decision    `json:"decision"`
	Run      *store.Run  `json:"run,omitempty"`
	Created  bool        `json:"created"`
}

type FixtureResult struct {
	Route    string   `json:"route"`
	Name     string   `json:"name"`
	Expect   string   `json:"expect"`
	Actual   string   `json:"actual"`
	Passed   bool     `json:"passed"`
	Decision Decision `json:"decision"`
}

type Service struct {
	Root      string
	Store     *store.Store
	Harnesses []string
	Wake      func()
}

func (service *Service) Definitions(ctx context.Context) ([]spec.Route, []spec.DefinitionIssue, []store.Subscription, error) {
	routes, issues := spec.DiscoverRoutes(service.Root)
	now := time.Now().UTC()
	for _, route := range routes {
		if err := service.Store.SyncSubscription(ctx, store.Subscription{ID: route.Name, Revision: routeRevision(route), Enabled: route.IsEnabled(), Priority: route.Priority, LoadedAt: now}); err != nil {
			return nil, issues, nil, err
		}
	}
	subscriptions, err := service.Store.ListSubscriptions(ctx)
	return routes, issues, subscriptions, err
}

// TestFixtures previews every checked-in route fixture without persisting an
// event. Fixtures intentionally use the production evaluator.
func (service *Service) TestFixtures(ctx context.Context) ([]FixtureResult, error) {
	routes, _ := spec.DiscoverRoutes(service.Root)
	results := []FixtureResult{}
	for _, route := range routes {
		for _, fixture := range route.Tests {
			data, err := json.Marshal(fixture.Event.Data)
			if err != nil {
				return nil, err
			}
			_, decision, err := service.Preview(ctx, Envelope{
				ID: stableID("fixture", route.Name, fixture.Name), Source: fixture.Event.Source,
				Type: fixture.Event.Type, Subject: fixture.Event.Subject, Data: data,
			})
			if err != nil {
				return nil, fmt.Errorf("route %q fixture %q: %w", route.Name, fixture.Name, err)
			}
			actual := decision.Selected
			if actual == "" {
				actual = "unrouted"
			}
			results = append(results, FixtureResult{Route: route.Name, Name: fixture.Name, Expect: fixture.Expect, Actual: actual, Passed: actual == fixture.Expect, Decision: decision})
		}
	}
	return results, nil
}
func (service *Service) SetRouteEnabled(ctx context.Context, name string, enabled bool) error {
	routes, _ := spec.DiscoverRoutes(service.Root)
	found := false
	for _, route := range routes {
		if route.Name == name {
			found = true
			if err := service.Store.SyncSubscription(ctx, store.Subscription{ID: route.Name, Revision: routeRevision(route), Enabled: route.IsEnabled(), Priority: route.Priority, LoadedAt: time.Now().UTC()}); err != nil {
				return err
			}
			break
		}
	}
	if !found {
		return store.ErrNotFound
	}
	return service.Store.SetSubscriptionEnabled(ctx, name, enabled, time.Now().UTC())
}

func Normalize(envelope Envelope, received time.Time) (store.Event, error) {
	envelope.SpecVersion = strings.TrimSpace(envelope.SpecVersion)
	if envelope.SpecVersion == "" {
		envelope.SpecVersion = "1.0"
	}
	if envelope.SpecVersion != "1.0" {
		return store.Event{}, errors.New("specversion must be 1.0")
	}
	envelope.ID = strings.TrimSpace(envelope.ID)
	envelope.Source = strings.TrimSpace(envelope.Source)
	envelope.Type = strings.TrimSpace(envelope.Type)
	envelope.Subject = strings.TrimSpace(envelope.Subject)
	if !envelopeIDPattern.MatchString(envelope.ID) || !envelopeIDPattern.MatchString(envelope.Source) {
		return store.Event{}, errors.New("event id and source must be bounded portable identifiers")
	}
	if envelope.Type == "" || len(envelope.Type) > 256 {
		return store.Event{}, errors.New("event type is required and must not exceed 256 characters")
	}
	if received.IsZero() {
		received = time.Now().UTC()
	}
	if envelope.Time.IsZero() {
		envelope.Time = received
	}
	if len(envelope.Data) == 0 {
		envelope.Data = json.RawMessage(`{}`)
	}
	if !json.Valid(envelope.Data) {
		return store.Event{}, errors.New("event data must be valid JSON")
	}
	normalized, err := Redact(envelope.Data)
	if err != nil {
		return store.Event{}, err
	}
	raw := envelope.RawData
	if len(raw) == 0 {
		raw = envelope.Data
	}
	redacted, err := Redact(raw)
	if err != nil {
		return store.Event{}, err
	}
	return store.Event{RecordID: stableID("evt", envelope.Source, envelope.ID), Source: envelope.Source, ExternalID: envelope.ID, Type: envelope.Type, Subject: envelope.Subject, OccurredAt: envelope.Time.UTC(), ReceivedAt: received.UTC(), Data: normalized, RawData: redacted}, nil
}

func Redact(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("raw event data must be valid JSON")
	}
	value = redactValue(value)
	data, err := json.Marshal(value)
	return data, err
}
func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			lower := strings.ToLower(key)
			if sensitiveKey(lower) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactValue(item)
			}
		}
		return out
	case []any:
		for index := range typed {
			typed[index] = redactValue(typed[index])
		}
		return typed
	default:
		return value
	}
}

func sensitiveKey(lower string) bool {
	for _, marker := range []string{"token", "secret", "password", "authorization", "cookie", "api_key", "apikey", "private_key", "privatekey", "credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func Evaluate(event store.Event, routes []spec.Route, enabled map[string]bool, issues []spec.DefinitionIssue) Decision {
	matches := []Match{}
	for _, route := range routes {
		active := route.IsEnabled()
		if value, ok := enabled[route.Name]; ok {
			active = value
		}
		if active && matchesRoute(event, route) {
			matches = append(matches, Match{Route: route.Name, Priority: route.Priority})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Priority == matches[j].Priority {
			return matches[i].Route < matches[j].Route
		}
		return matches[i].Priority > matches[j].Priority
	})
	decision := Decision{Matches: matches, Issues: issues}
	if len(matches) == 0 {
		decision.Reason = "no enabled route matched"
		return decision
	}
	highest := matches[0].Priority
	if len(matches) > 1 && matches[1].Priority == highest {
		decision.Reason = "highest priority tied"
		return decision
	}
	decision.Selected = matches[0].Route
	decision.Reason = "sole highest-priority route"
	return decision
}

func matchesRoute(event store.Event, route spec.Route) bool {
	if route.When.Source != "" && route.When.Source != event.Source {
		return false
	}
	if route.When.Type != "" && route.When.Type != event.Type {
		return false
	}
	var data map[string]any
	if len(route.When.Fields) > 0 && json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	for path, want := range route.When.Fields {
		got, ok := lookup(data, path)
		if !ok || !scalarEqual(got, want) {
			return false
		}
	}
	return true
}
func lookup(data map[string]any, path string) (any, bool) {
	current := any(data)
	for _, part := range strings.Split(strings.TrimPrefix(path, "data."), ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
func scalarEqual(left, right any) bool {
	normalize := func(value any) string {
		data, _ := json.Marshal(value)
		var decoded any
		_ = json.Unmarshal(data, &decoded)
		data, _ = json.Marshal(decoded)
		return string(data)
	}
	return normalize(left) == normalize(right)
}

func (service *Service) Preview(ctx context.Context, envelope Envelope) (store.Event, Decision, error) {
	event, err := Normalize(envelope, time.Now().UTC())
	if err != nil {
		return store.Event{}, Decision{}, err
	}
	decision, _, err := service.evaluate(ctx, event)
	return event, decision, err
}
func (service *Service) PreviewEvent(ctx context.Context, id string) (Decision, error) {
	event, err := service.Store.GetEvent(ctx, id)
	if err != nil {
		return Decision{}, err
	}
	decision, _, err := service.evaluate(ctx, event)
	return decision, err
}

func (service *Service) DispatchStored(ctx context.Context, id string) (Result, error) {
	event, err := service.Store.GetEvent(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if event.Status != store.EventIngested {
		return Result{Event: event}, nil
	}
	return service.dispatch(ctx, event, "", false)
}

func (service *Service) RecoverIngested(ctx context.Context) error {
	var failures []error
	for {
		events, err := service.Store.ListEventsByStatus(ctx, store.EventIngested, 500)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return errors.Join(failures...)
		}
		succeeded := 0
		for _, event := range events {
			if _, err := service.dispatch(ctx, event, "", false); err != nil {
				failures = append(failures, fmt.Errorf("dispatch event %s: %w", event.RecordID, err))
			} else {
				succeeded++
			}
		}
		if len(events) < 500 {
			return errors.Join(failures...)
		}
		// Every successfully handled event changed state. If all 500 failed,
		// returning avoids an infinite loop over the same durable page.
		if succeeded == 0 {
			return errors.Join(failures...)
		}
	}
}

func (service *Service) Ingest(ctx context.Context, envelope Envelope) (Result, error) {
	event, err := Normalize(envelope, time.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	stored, created, err := service.Store.IngestEvent(ctx, event)
	if err != nil {
		return Result{}, err
	}
	result := Result{Event: stored, Created: created}
	if !created && stored.Status != store.EventIngested {
		return result, nil
	}
	return service.dispatch(ctx, stored, "", created)
}

func (service *Service) RouteNow(ctx context.Context, eventID, routeName string) (Result, error) {
	event, err := service.Store.GetEvent(ctx, eventID)
	if err != nil {
		return Result{}, err
	}
	if event.Status == store.EventRouted {
		return Result{}, fmt.Errorf("%w: event is already routed", store.ErrConflict)
	}
	return service.dispatch(ctx, event, routeName, false)
}
func (service *Service) Reevaluate(ctx context.Context, eventID string) (Result, error) {
	if err := service.Store.ReopenEvent(ctx, eventID); err != nil {
		return Result{}, err
	}
	event, err := service.Store.GetEvent(ctx, eventID)
	if err != nil {
		return Result{}, err
	}
	return service.dispatch(ctx, event, "", false)
}
func (service *Service) Replay(ctx context.Context, eventID, replayID string) (Result, error) {
	original, err := service.Store.GetEvent(ctx, eventID)
	if err != nil {
		return Result{}, err
	}
	if !envelopeIDPattern.MatchString(replayID) {
		return Result{}, errors.New("replay id must be a portable identifier")
	}
	event := original
	event.RecordID = stableID("evt", original.Source, replayID)
	event.ExternalID = replayID
	event.ReplayOf = original.RecordID
	event.Status = store.EventIngested
	event.RoutedRunID = ""
	event.RoutingReason = ""
	event.ReceivedAt = time.Now().UTC()
	stored, created, err := service.Store.IngestEvent(ctx, event)
	if err != nil {
		return Result{}, err
	}
	if !created && stored.Status != store.EventIngested {
		return Result{Event: stored, Created: false}, nil
	}
	return service.dispatch(ctx, stored, "", created)
}

func (service *Service) dispatch(ctx context.Context, event store.Event, forced string, created bool) (Result, error) {
	decision, routes, err := service.evaluate(ctx, event)
	if err != nil {
		return Result{}, err
	}
	result := Result{Event: event, Decision: decision, Created: created}
	selected := decision.Selected
	if forced != "" {
		selected = forced
		decision.Selected = forced
		decision.Reason = "manually selected route"
		result.Decision = decision
	}
	if selected == "" {
		if event.Status == store.EventIngested {
			if err := service.Store.MarkEventUnrouted(ctx, event.RecordID, decision.Reason, time.Now().UTC()); err != nil {
				return Result{}, err
			}
			result.Event, _ = service.Store.GetEvent(ctx, event.RecordID)
		}
		return result, nil
	}
	var route *spec.Route
	for index := range routes {
		if routes[index].Name == selected {
			route = &routes[index]
			break
		}
	}
	if route == nil {
		return Result{}, store.ErrNotFound
	}
	run, err := service.runFor(ctx, event, *route)
	if err != nil {
		return Result{}, err
	}
	stored, _, err := service.Store.RouteEvent(ctx, event.RecordID, run, "route:"+route.Name)
	if err != nil {
		return Result{}, err
	}
	result.Run = &stored
	result.Event, _ = service.Store.GetEvent(ctx, event.RecordID)
	if service.Wake != nil {
		service.Wake()
	}
	return result, nil
}

func (service *Service) evaluate(ctx context.Context, event store.Event) (Decision, []spec.Route, error) {
	routes, issues := spec.DiscoverRoutes(service.Root)
	enabled := map[string]bool{}
	now := time.Now().UTC()
	for _, route := range routes {
		revision := routeRevision(route)
		if err := service.Store.SyncSubscription(ctx, store.Subscription{ID: route.Name, Revision: revision, Enabled: route.IsEnabled(), Priority: route.Priority, LoadedAt: now}); err != nil {
			return Decision{}, nil, err
		}
		value, err := service.Store.SubscriptionEnabled(ctx, route.Name, route.IsEnabled())
		if err != nil {
			return Decision{}, nil, err
		}
		enabled[route.Name] = value
	}
	return Evaluate(event, routes, enabled, issues), routes, nil
}

func (service *Service) runFor(ctx context.Context, event store.Event, route spec.Route) (store.Run, error) {
	team, err := spec.LoadTeam(service.Root, route.Invoke.Team)
	if err != nil {
		return store.Run{}, err
	}
	agentName := route.Invoke.Agent
	if agentName == "" {
		agentName = team.DefaultAgent
	}
	if agentName == "" || !contains(team.Agents, agentName) {
		return store.Run{}, fmt.Errorf("route %q agent is not a member of team %q", route.Name, team.Name)
	}
	agent, err := spec.LoadAgent(service.Root, agentName)
	if err != nil {
		return store.Run{}, err
	}
	local, err := spec.LoadLocal(service.Root)
	if err != nil {
		return store.Run{}, err
	}
	binding, ok := local.Workspaces[route.Invoke.Workspace]
	if !ok {
		return store.Run{}, fmt.Errorf("route %q workspace %q has no local binding", route.Name, route.Invoke.Workspace)
	}
	if info, statErr := os.Stat(binding.Path); statErr != nil || !info.IsDir() {
		return store.Run{}, fmt.Errorf("route %q workspace %q is unavailable", route.Name, route.Invoke.Workspace)
	}
	harness := route.Invoke.Harness
	if harness == "" {
		harness = "claude-code"
	}
	if len(service.Harnesses) > 0 && !contains(service.Harnesses, harness) {
		return store.Run{}, fmt.Errorf("route %q harness %q is unavailable", route.Name, harness)
	}
	permission := route.Invoke.Permission
	if permission == "" {
		permission = spec.PermissionReadonly
	}
	effective, err := spec.EffectivePermission(agent.MaxPermission, permission)
	if err != nil {
		return store.Run{}, err
	}
	now := time.Now().UTC()
	id := stableID("run", event.RecordID, route.Name)
	return store.Run{ID: id, IdempotencyKey: "event:" + event.RecordID + ":route:" + route.Name, EventRecordID: event.RecordID, Team: team.Name, Agent: agentName, Workspace: route.Invoke.Workspace, Harness: harness, Permission: effective, State: store.RunPending, CreatedAt: now, UpdatedAt: now}, nil
}

func routeRevision(route spec.Route) string {
	data, _ := json.Marshal(route)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func stableID(prefix string, parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(digest[:16])
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// ResolveDefinitionPath is used only for display; event data never participates.
func ResolveDefinitionPath(root, path string) string {
	return filepath.Join(root, filepath.FromSlash(path))
}
