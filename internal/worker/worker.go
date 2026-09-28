// Package worker claims durable runs and executes them through harness adapters.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mtfuller/agentworks/internal/approval"
	"github.com/mtfuller/agentworks/internal/harness"
	"github.com/mtfuller/agentworks/internal/memory"
	"github.com/mtfuller/agentworks/internal/monitors"
	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/providers"
	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
	awworkspace "github.com/mtfuller/agentworks/internal/workspace"
)

const (
	defaultLeaseTTL = 30 * time.Second
	completionGrace = 5 * time.Second
)

type Request struct {
	Run                store.Run
	Attempt            store.Attempt
	Event              store.Event
	WorkspacePath      string
	PermissionEndpoint string
	PermissionToken    string
	History            []store.TimelineEntry
	TranscriptExcerpts []string
}

type RetryDecision struct {
	Retry        bool
	Delay        time.Duration
	Reason       string
	SafeForWrite bool
}

type RetryPlanner interface {
	RetryDecision(context.Context, Request, awprocess.Result, error) RetryDecision
}

// Adapter converts a durable run into a supervised structured process.
type Adapter interface {
	Invocation(context.Context, Request) (awprocess.Spec, error)
}

// InvocationPreparer lets an adapter create ephemeral per-run configuration.
// cleanup is called after process completion, start failure, or cancellation.
type InvocationPreparer interface {
	PrepareInvocation(context.Context, Request) (spec awprocess.Spec, cleanup func(), err error)
}

type ApprovalRequester interface {
	ApprovalRequest(context.Context, Request) (*approval.Request, error)
}

type EventNormalizer interface {
	NormalizeLine([]byte) (harness.Event, bool)
}

type WorkspaceResolver func(alias string) (string, error)

type Config struct {
	Store             *store.Store
	Owner             string
	LogRoot           string
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	ResolveWorkspace  WorkspaceResolver
	Adapters          map[string]Adapter
	ApprovalGateway   *approval.Gateway
	ProjectRoot       string
}

type Worker struct {
	config Config
	mu     sync.Mutex
	active map[string]context.CancelFunc
}

func New(config Config) (*Worker, error) {
	if config.Store == nil {
		return nil, errors.New("worker store is required")
	}
	config.Owner = strings.TrimSpace(config.Owner)
	if config.Owner == "" {
		return nil, errors.New("worker owner is required")
	}
	if strings.TrimSpace(config.LogRoot) == "" {
		return nil, errors.New("worker log root is required")
	}
	if config.ResolveWorkspace == nil {
		return nil, errors.New("workspace resolver is required")
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = config.LeaseTTL / 3
	}
	if config.HeartbeatInterval >= config.LeaseTTL {
		return nil, errors.New("heartbeat interval must be shorter than lease TTL")
	}
	if config.Adapters == nil {
		config.Adapters = map[string]Adapter{}
	}
	return &Worker{config: config, active: map[string]context.CancelFunc{}}, nil
}

// RunOnce claims and executes at most one pending run.
func (worker *Worker) RunOnce(ctx context.Context) (bool, error) {
	now := time.Now().UTC()
	if _, err := worker.config.Store.RecoverExpiredLeases(ctx, now); err != nil {
		return false, err
	}
	if _, err := worker.config.Store.ActivateDueRetries(ctx, now, 100); err != nil {
		return false, err
	}
	harnesses := make([]string, 0, len(worker.config.Adapters))
	for harness := range worker.config.Adapters {
		harnesses = append(harnesses, harness)
	}
	sort.Strings(harnesses)
	claim, found, err := worker.config.Store.ClaimNextForHarnesses(ctx, worker.config.Owner, harnesses, now, worker.config.LeaseTTL)
	if err != nil || !found {
		return found, err
	}
	runContext, cancel := context.WithCancel(ctx)
	worker.mu.Lock()
	worker.active[claim.Run.ID] = cancel
	worker.mu.Unlock()
	defer func() {
		cancel()
		worker.mu.Lock()
		delete(worker.active, claim.Run.ID)
		worker.mu.Unlock()
	}()
	if err := worker.execute(runContext, claim); err != nil {
		return true, err
	}
	return true, nil
}

// Cancel stops an active run owned by this worker.
func (worker *Worker) Cancel(runID string) bool {
	worker.mu.Lock()
	cancel := worker.active[runID]
	worker.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (worker *Worker) execute(ctx context.Context, claim store.Claim) error {
	failBeforeStart := func(message string) error {
		completeCtx, cancel := context.WithTimeout(context.Background(), completionGrace)
		defer cancel()
		result, _ := json.Marshal(map[string]any{"started": false})
		if err := worker.config.Store.CompleteClaim(completeCtx, claim, store.RunFailed, message, result, time.Now().UTC()); err != nil {
			return fmt.Errorf("%s; persist failure: %w", message, err)
		}
		return errors.New(message)
	}
	if err := worker.config.Store.PrepareClaim(ctx, claim, time.Now().UTC()); err != nil {
		return fmt.Errorf("prepare claimed run: %w", err)
	}

	event, err := worker.config.Store.GetEvent(ctx, claim.Run.EventRecordID)
	if err != nil {
		return failBeforeStart("load run event")
	}
	workspace, err := worker.config.ResolveWorkspace(claim.Run.Workspace)
	if err != nil {
		return failBeforeStart("resolve run workspace")
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return failBeforeStart("run workspace is unavailable")
	}
	adapter := worker.config.Adapters[claim.Run.Harness]
	if adapter == nil {
		return failBeforeStart("run harness is unavailable")
	}
	request := Request{Run: claim.Run, Attempt: claim.Attempt, Event: event, WorkspacePath: workspace}
	request.History, err = worker.config.Store.ContextEntries(ctx, claim.Run)
	if err != nil {
		return failBeforeStart("load correlated run context")
	}
	request.TranscriptExcerpts = worker.transcriptExcerpts(ctx, request.History)
	if requester, ok := adapter.(ApprovalRequester); ok {
		approvalRequest, err := requester.ApprovalRequest(ctx, request)
		if err != nil {
			return failBeforeStart("prepare approval request")
		}
		if approvalRequest != nil {
			approved, approvalErr := (approval.Broker{
				Store: worker.config.Store, LeaseTTL: worker.config.LeaseTTL,
			}).Authorize(ctx, claim, *approvalRequest)
			if approvalErr != nil || !approved {
				conclusion := "approval denied"
				if approvalErr != nil {
					conclusion = "approval interrupted"
				}
				completeCtx, cancel := context.WithTimeout(context.Background(), completionGrace)
				defer cancel()
				result, _ := json.Marshal(map[string]any{"started": false, "approved": false})
				if err := worker.config.Store.CompleteClaim(completeCtx, claim, store.RunCancelled, conclusion, result, time.Now().UTC()); err != nil {
					return fmt.Errorf("%s; persist cancellation: %w", conclusion, err)
				}
				return nil
			}
		}
	}
	if worker.config.ApprovalGateway != nil && claim.Run.Permission != store.PermissionReadonly {
		credentials, err := worker.config.ApprovalGateway.Open(ctx, claim, workspace)
		if err != nil {
			return failBeforeStart("open run permission gateway")
		}
		defer credentials.Close()
		request.PermissionEndpoint = credentials.Endpoint
		request.PermissionToken = credentials.Token
	}
	beforeSnapshot, beforeSnapshotErr := awworkspace.Capture(workspace)
	specification, cleanup, err := prepareInvocation(ctx, adapter, request)
	if err != nil {
		return failBeforeStart("prepare harness invocation")
	}
	defer cleanup()
	specification.Dir = workspace
	if worker.config.ProjectRoot != "" && claim.Run.Team != "" {
		resolved, resolveErr := resolver.ResolveTeam(worker.config.ProjectRoot, claim.Run.Team, resolver.Options{AvailableProviders: []spec.Provider{
			spec.ProviderHost, spec.ProviderContainer, spec.ProviderRemote,
		}, SelectVariant: providers.ReadySelector(ctx, providers.Probe{})})
		if resolveErr != nil {
			return failBeforeStart("resolve immutable run plan")
		}
		planJSON, marshalErr := json.Marshal(resolved)
		if marshalErr != nil {
			return failBeforeStart("encode immutable run plan")
		}
		if saveErr := worker.config.Store.SaveExecutionPlan(ctx, store.ExecutionPlan{
			RunID: claim.Run.ID, PlanDigest: resolved.Digest, Provider: string(spec.ProviderHost),
			Runtime: specification.Executable, Network: "host", Plan: planJSON, CreatedAt: time.Now().UTC(),
		}); saveErr != nil {
			return failBeforeStart("store immutable run plan")
		}
	}
	log, err := openOutputLog(worker.config.LogRoot, claim.Run.ID, claim.Attempt.ID)
	if err != nil {
		return failBeforeStart("open run log")
	}
	originalSink := specification.Sink
	var normalizedText strings.Builder
	specification.Sink = func(output awprocess.Output) {
		log.Write(output)
		data, _ := json.Marshal(map[string]any{
			"attempt_id": claim.Attempt.ID, "output_sequence": output.Sequence, "stream": output.Stream,
		})
		_, _ = worker.config.Store.AppendRuntimeEvent(context.Background(), store.RuntimeEvent{
			Topic: "run.output", EntityType: "run", EntityID: claim.Run.ID, Data: data, CreatedAt: output.Time,
		})
		if normalizer, ok := adapter.(EventNormalizer); ok && output.Stream == awprocess.Stdout {
			for _, line := range bytes.Split(output.Data, []byte{'\n'}) {
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				if event, normalized := normalizer.NormalizeLine(line); normalized {
					if event.Text != "" && normalizedText.Len() < 1<<20 {
						remaining := (1 << 20) - normalizedText.Len()
						text := event.Text
						if len(text) > remaining {
							text = text[:remaining]
						}
						normalizedText.WriteString(text)
						normalizedText.WriteByte('\n')
					}
					// The SQLite journal is a small wakeup/audit index, not a
					// transcript. Text and vendor fragments remain only in the
					// redacted external log.
					event.Text = ""
					event.Data = nil
					eventData, _ := json.Marshal(map[string]any{"attempt_id": claim.Attempt.ID, "event": event})
					_, _ = worker.config.Store.AppendRuntimeEvent(context.Background(), store.RuntimeEvent{
						Topic: "harness.event", EntityType: "run", EntityID: claim.Run.ID, Data: eventData, CreatedAt: output.Time,
					})
				}
			}
		}
		if originalSink != nil {
			originalSink(output)
		}
	}

	child, err := awprocess.Start(ctx, specification)
	if err != nil {
		_, _ = log.Close()
		return failBeforeStart("start harness process")
	}
	startCtx, cancelStart := context.WithTimeout(context.Background(), completionGrace)
	startErr := worker.config.Store.StartAttempt(startCtx, claim, child.PID(), time.Now().UTC())
	cancelStart()
	if startErr != nil {
		child.Stop()
		_, _ = child.Wait()
		_, _ = log.Close()
		return fmt.Errorf("record harness start: %w", startErr)
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	heartbeatErrors := make(chan error, 1)
	go worker.heartbeat(heartbeatCtx, claim, child, heartbeatErrors)
	processResult, processErr := child.Wait()
	stopHeartbeat()
	heartbeatErr := <-heartbeatErrors
	logSummary, logErr := log.Close()
	afterSnapshot, afterSnapshotErr := awworkspace.Capture(workspace)
	changes := awworkspace.Changes{Added: []string{}, Modified: []string{}, Deleted: []string{}}
	if beforeSnapshotErr == nil && afterSnapshotErr == nil {
		changes = awworkspace.Compare(beforeSnapshot, afterSnapshot)
	} else {
		changes.Incomplete = true
	}
	if planner, ok := adapter.(RetryPlanner); ok && processErr != nil && heartbeatErr == nil &&
		!processResult.Cancelled && logErr == nil {
		decision := planner.RetryDecision(ctx, request, processResult, processErr)
		if decision.Retry && (claim.Run.Permission == store.PermissionReadonly || decision.SafeForWrite) {
			if decision.Delay < 0 {
				decision.Delay = 0
			}
			if strings.TrimSpace(decision.Reason) == "" {
				decision.Reason = "adapter requested retry"
			}
			resultJSON, _ := json.Marshal(map[string]any{
				"exit_code": processResult.ExitCode, "cancelled": false,
				"log": logSummary, "changes": changes, "retry": true,
			})
			completeCtx, cancel := context.WithTimeout(context.Background(), completionGrace)
			defer cancel()
			if err := worker.config.Store.ScheduleClaimRetry(completeCtx, claim, time.Now().UTC().Add(decision.Delay), decision.Reason, resultJSON, time.Now().UTC()); err != nil {
				return fmt.Errorf("schedule retry: %w", err)
			}
			return nil
		}
	}

	terminal := store.RunSucceeded
	conclusion := "harness completed"
	switch {
	case heartbeatErr != nil:
		terminal = store.RunInterrupted
		conclusion = "worker lost its lease"
	case processResult.Cancelled || errors.Is(processErr, context.Canceled) || errors.Is(processErr, awprocess.ErrStopped):
		terminal = store.RunCancelled
		conclusion = "harness cancelled"
	case processErr != nil:
		terminal = store.RunFailed
		conclusion = fmt.Sprintf("harness exited with code %d", processResult.ExitCode)
	case logErr != nil:
		terminal = store.RunFailed
		conclusion = "run log could not be finalized"
	}
	resultJSON, _ := json.Marshal(map[string]any{
		"exit_code": processResult.ExitCode, "cancelled": processResult.Cancelled,
		"log": logSummary, "changes": changes,
	})
	completeCtx, cancel := context.WithTimeout(context.Background(), completionGrace)
	defer cancel()
	if err := worker.config.Store.CompleteClaim(completeCtx, claim, terminal, conclusion, resultJSON, time.Now().UTC()); err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	structuredConclusion, structured := harness.ExtractConclusion(normalizedText.String())
	if !structured {
		structuredConclusion.Normalize()
	}
	privateMemory := strings.Join(structuredConclusion.OutstandingWork, "\n")
	if err := worker.config.Store.SaveRunConclusion(completeCtx, store.StoredConclusion{
		RunID: claim.Run.ID, Conclusion: structuredConclusion, PrivateMemory: privateMemory,
		Structured: structured, CreatedAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("store structured conclusion: %w", err)
	}
	if structured && len(structuredConclusion.SuggestedMemory) != 0 && worker.config.ProjectRoot != "" {
		agent, loadErr := spec.LoadAgent(worker.config.ProjectRoot, claim.Run.Agent)
		if loadErr == nil && agent.Memory != "" {
			target := strings.TrimSpace(agent.Memory)
			before, readErr := os.ReadFile(filepath.Join(worker.config.ProjectRoot, filepath.FromSlash(target)))
			if readErr == nil {
				proposal := memory.AppendProposal(claim.Run.ID+":memory:agent", claim.Run.ID, "agent", target, before, structuredConclusion.SuggestedMemory, time.Now().UTC())
				if err := worker.config.Store.CreateMemoryProposal(completeCtx, proposal); err != nil {
					return fmt.Errorf("store memory proposal: %w", err)
				}
			}
		}
	}
	if err := monitors.EvaluateAndStore(completeCtx, worker.config.Store, time.Now().UTC()); err != nil {
		return fmt.Errorf("evaluate runtime monitors: %w", err)
	}
	return nil
}

func prepareInvocation(ctx context.Context, adapter Adapter, request Request) (awprocess.Spec, func(), error) {
	if preparer, ok := adapter.(InvocationPreparer); ok {
		specification, cleanup, err := preparer.PrepareInvocation(ctx, request)
		if cleanup == nil {
			cleanup = func() {}
		}
		return specification, cleanup, err
	}
	specification, err := adapter.Invocation(ctx, request)
	return specification, func() {}, err
}

func (worker *Worker) heartbeat(ctx context.Context, claim store.Claim, child *awprocess.Process, result chan<- error) {
	ticker := time.NewTicker(worker.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			result <- nil
			return
		case now := <-ticker.C:
			if err := worker.config.Store.HeartbeatLease(context.Background(), claim, now.UTC(), worker.config.LeaseTTL); err != nil {
				child.Stop()
				result <- err
				return
			}
		}
	}
}
