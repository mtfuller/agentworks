package store

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/mtfuller/agentworks/internal/spec"
)

var (
	// ErrNotFound means the requested durable record does not exist.
	ErrNotFound = errors.New("runtime record not found")
	// ErrConflict means durable state changed or an idempotency key was reused incompatibly.
	ErrConflict = errors.New("runtime state conflict")
)

type EventStatus string

const (
	EventIngested EventStatus = "ingested"
	EventRouted   EventStatus = "routed"
	EventUnrouted EventStatus = "unrouted"
)

// Event is a normalized, deduplicated event envelope.
type Event struct {
	RecordID      string          `json:"record_id"`
	Source        string          `json:"source"`
	ExternalID    string          `json:"external_id"`
	Type          string          `json:"type"`
	Subject       string          `json:"subject,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
	ReceivedAt    time.Time       `json:"received_at"`
	Data          json.RawMessage `json:"data"`
	RawData       json.RawMessage `json:"raw_data"`
	Status        EventStatus     `json:"status"`
	RoutingReason string          `json:"routing_reason,omitempty"`
	RoutedRunID   string          `json:"routed_run_id,omitempty"`
	WorkItemID    string          `json:"work_item_id,omitempty"`
	ReplayOf      string          `json:"replay_of,omitempty"`
}

type Subscription struct {
	ID       string    `json:"id"`
	Revision string    `json:"revision"`
	Enabled  bool      `json:"enabled"`
	Priority int       `json:"priority"`
	LoadedAt time.Time `json:"loaded_at"`
}

type Timer struct {
	ID             string          `json:"id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Kind           string          `json:"kind"`
	DueAt          time.Time       `json:"due_at"`
	State          string          `json:"state"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type SourceStatus struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	ConfigRevision    string    `json:"config_revision"`
	State             string    `json:"state"`
	DefinitionEnabled bool      `json:"definition_enabled"`
	Paused            bool      `json:"paused"`
	LastError         string    `json:"last_error,omitempty"`
	Cursor            string    `json:"cursor,omitempty"`
	LastPollAt        time.Time `json:"last_poll_at,omitempty"`
	LastSuccessAt     time.Time `json:"last_success_at,omitempty"`
	NextPollAt        time.Time `json:"next_poll_at,omitempty"`
	RateLimitResetAt  time.Time `json:"rate_limit_reset_at,omitempty"`
	FailureCount      int       `json:"failure_count"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (source SourceStatus) Enabled() bool { return source.DefinitionEnabled && !source.Paused }

type PollCommit struct {
	SourceID         string
	AttemptID        string
	ExpectedCursor   string
	Cursor           string
	Events           []Event
	WorkItems        []WorkItem
	Outcomes         []Outcome
	PolledAt         time.Time
	NextPollAt       time.Time
	RateLimitResetAt time.Time
}

type PollCommitResult struct {
	EventRecordIDs   []string
	EventsInserted   int
	OutcomesInserted int
}

// SourcePollAttempt is a redacted operational record for one connector poll.
// It deliberately contains counts, identifiers, and error classification—not
// request payloads, headers, or credential material.
type SourcePollAttempt struct {
	ID               string    `json:"id"`
	SourceID         string    `json:"source_id"`
	State            string    `json:"state"`
	CursorBefore     string    `json:"cursor_before,omitempty"`
	CursorAfter      string    `json:"cursor_after,omitempty"`
	EventsObserved   int       `json:"events_observed"`
	EventsInserted   int       `json:"events_inserted"`
	OutcomesObserved int       `json:"outcomes_observed"`
	OutcomesInserted int       `json:"outcomes_inserted"`
	RoutedRuns       int       `json:"routed_runs"`
	EventRecordIDs   []string  `json:"event_record_ids"`
	RunIDs           []string  `json:"run_ids"`
	ErrorCode        string    `json:"error_code,omitempty"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at,omitempty"`
	NextPollAt       time.Time `json:"next_poll_at,omitempty"`
	RateLimitResetAt time.Time `json:"rate_limit_reset_at,omitempty"`
}

type Permission = spec.Permission

const (
	PermissionReadonly    = spec.PermissionReadonly
	PermissionReadwrite   = spec.PermissionReadwrite
	PermissionCollaborate = spec.PermissionCollaborate
	PermissionAutonomous  = spec.PermissionAutonomous
)

type RunState string

const (
	RunPending            RunState = "pending"
	RunLeased             RunState = "leased"
	RunPreparing          RunState = "preparing"
	RunRunning            RunState = "running"
	RunWaitingForApproval RunState = "waiting-for-approval"
	RunRetryScheduled     RunState = "retry-scheduled"
	RunSucceeded          RunState = "succeeded"
	RunFailed             RunState = "failed"
	RunCancelled          RunState = "cancelled"
	RunInterrupted        RunState = "interrupted"
)

// Run is one durable agent invocation. Attempts and process details are separate records.
type Run struct {
	ID             string     `json:"id"`
	IdempotencyKey string     `json:"idempotency_key"`
	EventRecordID  string     `json:"event_record_id,omitempty"`
	ParentRunID    string     `json:"parent_run_id,omitempty"`
	Team           string     `json:"team,omitempty"`
	Agent          string     `json:"agent"`
	Workspace      string     `json:"workspace"`
	Harness        string     `json:"harness"`
	Permission     Permission `json:"permission"`
	State          RunState   `json:"state"`
	Conclusion     string     `json:"conclusion,omitempty"`
	Pinned         bool       `json:"pinned"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ExecutionPlan is the immutable provider/runtime snapshot used by one run.
// Plan contains the vendor-neutral resolved team plan, not credentials.
type ExecutionPlan struct {
	RunID          string          `json:"run_id"`
	PlanDigest     string          `json:"plan_digest"`
	Provider       string          `json:"provider"`
	Runtime        string          `json:"runtime"`
	RuntimeVersion string          `json:"runtime_version,omitempty"`
	ImageDigest    string          `json:"image_digest,omitempty"`
	Network        string          `json:"network"`
	Plan           json.RawMessage `json:"plan"`
	CreatedAt      time.Time       `json:"created_at"`
}

// RunConclusion is the portable handoff record produced by a harness run.
// Arrays are initialized by Normalize so JSON never exposes ambiguous nulls.
type RunConclusion struct {
	Completed       []string `json:"completed"`
	FilesChanged    []string `json:"files_changed"`
	CommandsRun     []string `json:"commands_run"`
	Checks          []string `json:"checks"`
	ExternalActions []string `json:"external_actions"`
	OutstandingWork []string `json:"outstanding_work"`
	SuggestedMemory []string `json:"suggested_memory"`
}

func (conclusion *RunConclusion) Normalize() {
	if conclusion.Completed == nil {
		conclusion.Completed = []string{}
	}
	if conclusion.FilesChanged == nil {
		conclusion.FilesChanged = []string{}
	}
	if conclusion.CommandsRun == nil {
		conclusion.CommandsRun = []string{}
	}
	if conclusion.Checks == nil {
		conclusion.Checks = []string{}
	}
	if conclusion.ExternalActions == nil {
		conclusion.ExternalActions = []string{}
	}
	if conclusion.OutstandingWork == nil {
		conclusion.OutstandingWork = []string{}
	}
	if conclusion.SuggestedMemory == nil {
		conclusion.SuggestedMemory = []string{}
	}
}

type StoredConclusion struct {
	RunID         string        `json:"run_id"`
	Conclusion    RunConclusion `json:"conclusion"`
	PrivateMemory string        `json:"private_memory,omitempty"`
	Structured    bool          `json:"structured"`
	CreatedAt     time.Time     `json:"created_at"`
}

type WorkItem struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	ExternalKey string          `json:"external_key"`
	Title       string          `json:"title,omitempty"`
	State       string          `json:"state,omitempty"`
	Data        json.RawMessage `json:"data"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type Outcome struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Subject       string          `json:"subject,omitempty"`
	EventRecordID string          `json:"event_record_id,omitempty"`
	RunID         string          `json:"run_id,omitempty"`
	WorkItemID    string          `json:"work_item_id,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Data          json.RawMessage `json:"data"`
}

type TimelineEntry struct {
	Kind       string          `json:"kind"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	State      string          `json:"state,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type MemoryProposalState string

const (
	MemoryProposalPending  MemoryProposalState = "pending"
	MemoryProposalApproved MemoryProposalState = "approved"
	MemoryProposalRejected MemoryProposalState = "rejected"
	MemoryProposalConflict MemoryProposalState = "conflict"
)

type MemoryProposal struct {
	ID             string              `json:"id"`
	RunID          string              `json:"run_id"`
	Scope          string              `json:"scope"`
	TargetPath     string              `json:"target_path"`
	BaseSHA256     string              `json:"base_sha256"`
	BeforeMarkdown string              `json:"before_markdown"`
	AfterMarkdown  string              `json:"after_markdown"`
	Diff           string              `json:"diff"`
	State          MemoryProposalState `json:"state"`
	CreatedAt      time.Time           `json:"created_at"`
	DecidedAt      *time.Time          `json:"decided_at,omitempty"`
}

type MonitorState struct {
	MonitorID   string          `json:"monitor_id"`
	Revision    string          `json:"revision"`
	State       string          `json:"state"`
	Window      json.RawMessage `json:"window"`
	Details     json.RawMessage `json:"details"`
	EvaluatedAt time.Time       `json:"evaluated_at"`
}

type RuntimeMetrics struct {
	PendingRuns       int           `json:"pending_runs"`
	OldestQueueDelay  time.Duration `json:"oldest_queue_delay"`
	Attempts          int           `json:"attempts"`
	RetryAttempts     int           `json:"retry_attempts"`
	CompletedRuns     int           `json:"completed_runs"`
	AverageDuration   time.Duration `json:"average_duration"`
	ConfiguredSources int           `json:"configured_sources"`
	StaleSources      int           `json:"stale_sources"`
}

type ApprovalState string

const (
	ApprovalPending   ApprovalState = "pending"
	ApprovalApproved  ApprovalState = "approved"
	ApprovalDenied    ApprovalState = "denied"
	ApprovalCancelled ApprovalState = "cancelled"
	ApprovalExpired   ApprovalState = "expired"
)

type Approval struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	AttemptID      string          `json:"attempt_id,omitempty"`
	Kind           string          `json:"kind"`
	Summary        string          `json:"summary"`
	Scope          json.RawMessage `json:"scope"`
	State          ApprovalState   `json:"state"`
	ResumeState    RunState        `json:"-"`
	RequestedAt    time.Time       `json:"requested_at"`
	DecidedAt      *time.Time      `json:"decided_at,omitempty"`
	DecisionReason string          `json:"decision_reason,omitempty"`
}

var runTransitions = map[RunState]map[RunState]bool{
	RunPending: {
		RunLeased: true, RunCancelled: true,
	},
	RunLeased: {
		RunPending: true, RunPreparing: true, RunCancelled: true, RunInterrupted: true,
	},
	RunPreparing: {
		RunRunning: true, RunFailed: true, RunCancelled: true, RunInterrupted: true,
	},
	RunRunning: {
		RunWaitingForApproval: true, RunRetryScheduled: true, RunSucceeded: true,
		RunFailed: true, RunCancelled: true, RunInterrupted: true,
	},
	RunWaitingForApproval: {
		RunRunning: true, RunFailed: true, RunCancelled: true, RunInterrupted: true,
	},
	RunRetryScheduled: {
		RunPending: true, RunCancelled: true,
	},
}

func validTransition(from, to RunState) bool {
	return runTransitions[from][to]
}
