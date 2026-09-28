package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreateRun stores an idempotent run. A duplicate idempotency key returns the
// original record and created=false, provided its immutable identity matches.
func (s *Store) CreateRun(ctx context.Context, run Run) (stored Run, created bool, err error) {
	if err := normalizeRun(&run); err != nil {
		return Run{}, false, err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (
			id, idempotency_key, event_record_id, parent_run_id, team, agent, workspace,
			harness, permission, state, conclusion, pinned, created_at, updated_at
		) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`, run.ID, run.IdempotencyKey, run.EventRecordID, run.ParentRunID, run.Team,
		run.Agent, run.Workspace, run.Harness, run.Permission, run.State, run.Conclusion, run.Pinned,
		millis(run.CreatedAt), millis(run.UpdatedAt))
	if err != nil {
		return Run{}, false, fmt.Errorf("create run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Run{}, false, fmt.Errorf("inspect run insert: %w", err)
	}
	stored, err = s.runByIdempotencyKey(ctx, run.IdempotencyKey)
	if err != nil {
		return Run{}, false, err
	}
	if rows == 0 && !sameRunIdentity(stored, run) {
		return Run{}, false, fmt.Errorf("%w: idempotency key %q belongs to run %q", ErrConflict, run.IdempotencyKey, stored.ID)
	}
	return stored, rows == 1, nil
}

// RouteEvent atomically creates one idempotent run and attaches it to an event.
// Repeating the exact operation returns the existing run.
func (s *Store) RouteEvent(ctx context.Context, recordID string, run Run, reason string) (stored Run, created bool, err error) {
	if strings.TrimSpace(recordID) == "" {
		return Run{}, false, errors.New("event record ID is required")
	}
	run.EventRecordID = recordID
	if err := normalizeRun(&run); err != nil {
		return Run{}, false, err
	}
	// Routing is a compound read/write transaction. Studio has one store and one
	// scheduler, so serialize these short transactions to avoid SQLite snapshot
	// upgrade conflicts while retaining concurrent WAL readers.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, fmt.Errorf("begin event route: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stored, created, err = routeEventTx(ctx, tx, recordID, run, reason)
	if err != nil {
		return Run{}, false, err
	}
	if created {
		if err = appendRunStateEvent(ctx, tx, stored.ID, stored.State, "", stored.CreatedAt); err != nil {
			return Run{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Run{}, false, fmt.Errorf("commit event route: %w", err)
	}
	return stored, created, nil
}

// IngestAndRoute atomically deduplicates a normalized event and attaches its
// sole idempotent run. It is the command-side boundary used by manual requests.
func (s *Store) IngestAndRoute(ctx context.Context, event Event, run Run, reason string) (stored Run, created bool, err error) {
	if err := normalizeEvent(&event); err != nil {
		return Run{}, false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, fmt.Errorf("begin event ingestion and route: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO events (
			record_id, source, external_id, type, subject, occurred_at, received_at,
			data_json, raw_data_json, status, routing_reason, routed_run_id, work_item_id, replay_of
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))
		ON CONFLICT(source, external_id) DO NOTHING
	`, event.RecordID, event.Source, event.ExternalID, event.Type, event.Subject,
		millis(event.OccurredAt), millis(event.ReceivedAt), string(event.Data), string(event.RawData), event.Status,
		event.RoutingReason, event.RoutedRunID, event.WorkItemID, event.ReplayOf); err != nil {
		return Run{}, false, fmt.Errorf("ingest routed event: %w", err)
	}
	var recordID string
	if err = tx.QueryRowContext(ctx,
		`SELECT record_id FROM events WHERE source = ? AND external_id = ?`, event.Source, event.ExternalID,
	).Scan(&recordID); err != nil {
		return Run{}, false, fmt.Errorf("read ingested event: %w", err)
	}
	run.EventRecordID = recordID
	if err = normalizeRun(&run); err != nil {
		return Run{}, false, err
	}
	stored, created, err = routeEventTx(ctx, tx, recordID, run, reason)
	if err != nil {
		return Run{}, false, err
	}
	if created {
		if err = appendRunStateEvent(ctx, tx, stored.ID, stored.State, "", stored.CreatedAt); err != nil {
			return Run{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Run{}, false, fmt.Errorf("commit event ingestion and route: %w", err)
	}
	return stored, created, nil
}

func routeEventTx(ctx context.Context, tx *sql.Tx, recordID string, run Run, reason string) (Run, bool, error) {
	var status EventStatus
	var routedRunID string
	if err := tx.QueryRowContext(ctx,
		`SELECT status, COALESCE(routed_run_id, '') FROM events WHERE record_id = ?`, recordID,
	).Scan(&status, &routedRunID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, false, ErrNotFound
		}
		return Run{}, false, fmt.Errorf("read event for route: %w", err)
	}
	if status == EventRouted {
		stored, err := scanRun(tx.QueryRowContext(ctx, runSelect+` WHERE id = ?`, routedRunID))
		if err != nil {
			return Run{}, false, err
		}
		if !sameRunIdentity(stored, run) {
			return Run{}, false, fmt.Errorf("%w: event %q is already routed to run %q", ErrConflict, recordID, routedRunID)
		}
		return stored, false, nil
	}

	result, execErr := tx.ExecContext(ctx, `
		INSERT INTO runs (
			id, idempotency_key, event_record_id, parent_run_id, team, agent, workspace,
			harness, permission, state, conclusion, pinned, created_at, updated_at
		) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`, run.ID, run.IdempotencyKey, recordID, run.ParentRunID, run.Team, run.Agent,
		run.Workspace, run.Harness, run.Permission, run.State, run.Conclusion, run.Pinned,
		millis(run.CreatedAt), millis(run.UpdatedAt))
	if execErr != nil {
		return Run{}, false, fmt.Errorf("create routed run: %w", execErr)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return Run{}, false, fmt.Errorf("inspect routed run insert: %w", rowsErr)
	}
	stored, err := scanRun(tx.QueryRowContext(ctx, runSelect+` WHERE idempotency_key = ?`, run.IdempotencyKey))
	if err != nil {
		return Run{}, false, err
	}
	if rows == 0 && !sameRunIdentity(stored, run) {
		return Run{}, false, fmt.Errorf("%w: idempotency key %q belongs to run %q", ErrConflict, run.IdempotencyKey, stored.ID)
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE events SET status = ?, routing_reason = NULLIF(?, ''),
			routing_decided_at = ?, routed_run_id = ?
		WHERE record_id = ?
	`, EventRouted, reason, millis(time.Now().UTC()), stored.ID, recordID); err != nil {
		return Run{}, false, fmt.Errorf("attach routed run: %w", err)
	}
	eventData, _ := json.Marshal(map[string]any{"status": EventRouted, "run_id": stored.ID, "reason": reason})
	if _, err = appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "event.routing", EntityType: "event", EntityID: recordID, Data: eventData, CreatedAt: time.Now().UTC()}); err != nil {
		return Run{}, false, err
	}
	return stored, rows == 1, nil
}

// GetRun loads a run by durable ID.
func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, runSelect+` WHERE id = ?`, id))
}

// ListRuns returns newest runs first. Limit is clamped to 1..500.
func (s *Store) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 100
	} else if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, runSelect+` ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	runs := make([]Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

// ListAttempts returns a run's attempts in execution order.
func (s *Store) ListAttempts(ctx context.Context, runID string) ([]Attempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, number, worker_id, COALESCE(process_id, 0), state,
		       started_at, finished_at, COALESCE(result_json, '')
		FROM run_attempts WHERE run_id = ? ORDER BY number
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list run attempts: %w", err)
	}
	defer rows.Close()
	attempts := make([]Attempt, 0)
	for rows.Next() {
		var attempt Attempt
		var startedAt, finishedAt sql.NullInt64
		var result string
		if err := rows.Scan(&attempt.ID, &attempt.RunID, &attempt.Number, &attempt.WorkerID,
			&attempt.ProcessID, &attempt.State, &startedAt, &finishedAt, &result); err != nil {
			return nil, fmt.Errorf("scan run attempt: %w", err)
		}
		attempt.StartedAt = optionalTime(startedAt)
		attempt.FinishedAt = optionalTime(finishedAt)
		if result != "" {
			attempt.Result = json.RawMessage(result)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list run attempts: %w", err)
	}
	return attempts, nil
}

// CancelPendingRun cancels work that is waiting for either its first attempt or
// a scheduled retry. Active work is cancelled through its owning worker so
// process termination and lease cleanup remain coordinated.
func (s *Store) CancelPendingRun(ctx context.Context, id string, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin pending cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, conclusion = ?, updated_at = ?
		WHERE id = ? AND state IN (?, ?)
	`, RunCancelled, "cancelled before next execution", millis(now), id, RunPending, RunRetryScheduled)
	if err != nil {
		return false, fmt.Errorf("cancel pending run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect pending cancellation: %w", err)
	}
	if changed == 1 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE timers SET state = 'cancelled', updated_at = ?
			WHERE kind = 'run-retry' AND state = 'pending'
				AND json_extract(payload_json, '$.run_id') = ?
		`, millis(now), id); err != nil {
			return false, fmt.Errorf("cancel pending retry timer: %w", err)
		}
		if err := appendRunStateEvent(ctx, tx, id, RunCancelled, "", now); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit pending cancellation: %w", err)
		}
		return true, nil
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE id = ?`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect pending cancellation: %w", err)
	}
	if exists == 0 {
		return false, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit unchanged cancellation: %w", err)
	}
	if _, err := s.GetRun(ctx, id); err != nil {
		return false, err
	}
	return false, nil
}

func optionalTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := time.UnixMilli(value.Int64).UTC()
	return &timestamp
}

// TransitionRun performs an optimistic state transition.
func (s *Store) TransitionRun(ctx context.Context, id string, from, to RunState, at time.Time) error {
	if !validTransition(from, to) {
		return fmt.Errorf("invalid run transition %s -> %s", from, to)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE runs SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		to, millis(at), id, from)
	if err != nil {
		return fmt.Errorf("transition run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect run transition: %w", err)
	}
	if rows == 1 {
		return nil
	}
	var state RunState
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM runs WHERE id = ?`, id).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("inspect run state: %w", err)
	}
	return fmt.Errorf("%w: run %q is %s, expected %s", ErrConflict, id, state, from)
}

const runSelect = `
	SELECT id, idempotency_key, COALESCE(event_record_id, ''), COALESCE(parent_run_id, ''),
		COALESCE(team, ''), agent, workspace, harness, permission, state, COALESCE(conclusion, ''),
		pinned, created_at, updated_at
	FROM runs`

func (s *Store) runByIdempotencyKey(ctx context.Context, key string) (Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, runSelect+` WHERE idempotency_key = ?`, key))
}

func scanRun(row rowScanner) (Run, error) {
	var run Run
	var createdAt, updatedAt int64
	if err := row.Scan(&run.ID, &run.IdempotencyKey, &run.EventRecordID, &run.ParentRunID,
		&run.Team, &run.Agent, &run.Workspace, &run.Harness, &run.Permission, &run.State,
		&run.Conclusion, &run.Pinned, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("scan run: %w", err)
	}
	run.CreatedAt = fromMillis(createdAt)
	run.UpdatedAt = fromMillis(updatedAt)
	return run, nil
}

func normalizeRun(run *Run) error {
	run.ID = strings.TrimSpace(run.ID)
	run.IdempotencyKey = strings.TrimSpace(run.IdempotencyKey)
	run.Agent = strings.TrimSpace(run.Agent)
	run.Workspace = strings.TrimSpace(run.Workspace)
	run.Harness = strings.TrimSpace(run.Harness)
	if run.ID == "" || run.IdempotencyKey == "" || run.Agent == "" || run.Workspace == "" || run.Harness == "" {
		return errors.New("run ID, idempotency key, agent, workspace, and harness are required")
	}
	if run.Permission == "" {
		run.Permission = PermissionReadonly
	}
	if run.Permission != PermissionReadonly && run.Permission != PermissionReadwrite && run.Permission != PermissionCollaborate && run.Permission != PermissionAutonomous {
		return fmt.Errorf("invalid run permission %q", run.Permission)
	}
	if run.State == "" {
		run.State = RunPending
	}
	if _, valid := runTransitions[run.State]; !valid && run.State != RunSucceeded && run.State != RunFailed && run.State != RunCancelled && run.State != RunInterrupted {
		return fmt.Errorf("invalid run state %q", run.State)
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	return nil
}

func sameRunIdentity(a, b Run) bool {
	return a.IdempotencyKey == b.IdempotencyKey &&
		a.EventRecordID == b.EventRecordID &&
		a.ParentRunID == b.ParentRunID &&
		a.Team == b.Team &&
		a.Agent == b.Agent &&
		a.Workspace == b.Workspace &&
		a.Harness == b.Harness &&
		a.Permission == b.Permission
}
