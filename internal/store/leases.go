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

type LeaseMode string

const (
	LeaseReadonly LeaseMode = "readonly"
	LeaseWrite    LeaseMode = "write"
)

type Attempt struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id"`
	Number     int             `json:"number"`
	WorkerID   string          `json:"worker_id"`
	ProcessID  int             `json:"process_id,omitempty"`
	State      RunState        `json:"state"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

type Lease struct {
	RunID       string    `json:"run_id"`
	Workspace   string    `json:"workspace"`
	Mode        LeaseMode `json:"mode"`
	Owner       string    `json:"owner"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Claim struct {
	Run     Run     `json:"run"`
	Attempt Attempt `json:"attempt"`
	Lease   Lease   `json:"lease"`
}

// ClaimNext claims the oldest runnable pending run whose workspace lease is
// compatible. Readonly claims may overlap each other; write-capable claims are
// exclusive with every live claim in the workspace.
func (s *Store) ClaimNext(ctx context.Context, owner string, now time.Time, ttl time.Duration) (claim Claim, found bool, err error) {
	return s.ClaimNextForHarnesses(ctx, owner, nil, now, ttl)
}

// ClaimNextForHarnesses is ClaimNext constrained to harnesses this worker can
// execute. An empty list claims nothing; nil preserves ClaimNext's all-harness
// behavior for callers that intentionally provide a universal worker.
func (s *Store) ClaimNextForHarnesses(ctx context.Context, owner string, harnesses []string, now time.Time, ttl time.Duration) (claim Claim, found bool, err error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return Claim{}, false, errors.New("lease owner is required")
	}
	if ttl <= 0 {
		return Claim{}, false, errors.New("lease TTL must be greater than zero")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, false, fmt.Errorf("begin run claim: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, runSelect+` WHERE state = ? ORDER BY created_at, id`, RunPending)
	if err != nil {
		return Claim{}, false, fmt.Errorf("list pending runs: %w", err)
	}
	var candidates []Run
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			rows.Close()
			return Claim{}, false, scanErr
		}
		candidates = append(candidates, run)
	}
	if err = rows.Close(); err != nil {
		return Claim{}, false, fmt.Errorf("close pending runs: %w", err)
	}
	if err = rows.Err(); err != nil {
		return Claim{}, false, fmt.Errorf("list pending runs: %w", err)
	}

	for _, run := range candidates {
		if harnesses != nil && !containsHarness(harnesses, run.Harness) {
			continue
		}
		mode := leaseModeFor(run.Permission)
		compatible, checkErr := workspaceLeaseAvailable(ctx, tx, run.Workspace, mode, now)
		if checkErr != nil {
			return Claim{}, false, checkErr
		}
		if !compatible {
			continue
		}
		claim, err = claimRun(ctx, tx, run, owner, mode, now, ttl)
		if err != nil {
			return Claim{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return Claim{}, false, fmt.Errorf("commit run claim: %w", err)
		}
		return claim, true, nil
	}
	if err = tx.Commit(); err != nil {
		return Claim{}, false, fmt.Errorf("commit empty run claim: %w", err)
	}
	return Claim{}, false, nil
}

func containsHarness(harnesses []string, wanted string) bool {
	for _, harness := range harnesses {
		if harness == wanted {
			return true
		}
	}
	return false
}

func workspaceLeaseAvailable(ctx context.Context, tx *sql.Tx, workspace string, mode LeaseMode, now time.Time) (bool, error) {
	query := `SELECT COUNT(*) FROM run_leases WHERE workspace = ? AND expires_at > ?`
	args := []any{workspace, millis(now)}
	if mode == LeaseReadonly {
		query += ` AND mode = ?`
		args = append(args, LeaseWrite)
	}
	var conflicts int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&conflicts); err != nil {
		return false, fmt.Errorf("check workspace lease: %w", err)
	}
	return conflicts == 0, nil
}

func claimRun(ctx context.Context, tx *sql.Tx, run Run, owner string, mode LeaseMode, now time.Time, ttl time.Duration) (Claim, error) {
	result, err := tx.ExecContext(ctx,
		`UPDATE runs SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		RunLeased, millis(now), run.ID, RunPending)
	if err != nil {
		return Claim{}, fmt.Errorf("mark run leased: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Claim{}, fmt.Errorf("inspect run claim: %w", err)
	}
	if changed != 1 {
		return Claim{}, fmt.Errorf("%w: run %q is no longer pending", ErrConflict, run.ID)
	}

	var attemptNumber int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM run_attempts WHERE run_id = ?`, run.ID,
	).Scan(&attemptNumber); err != nil {
		return Claim{}, fmt.Errorf("choose attempt number: %w", err)
	}
	attemptID := fmt.Sprintf("%s:attempt:%d", run.ID, attemptNumber)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO run_attempts(id, run_id, number, worker_id, state)
		VALUES (?, ?, ?, ?, ?)
	`, attemptID, run.ID, attemptNumber, owner, RunPreparing); err != nil {
		return Claim{}, fmt.Errorf("create run attempt: %w", err)
	}
	expiresAt := now.Add(ttl)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO run_leases(run_id, workspace, mode, owner, acquired_at, heartbeat_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.Workspace, mode, owner, millis(now), millis(now), millis(expiresAt)); err != nil {
		return Claim{}, fmt.Errorf("create run lease: %w", err)
	}
	run.State = RunLeased
	run.UpdatedAt = now
	if err := appendRunStateEvent(ctx, tx, run.ID, RunLeased, attemptID, now); err != nil {
		return Claim{}, err
	}
	return Claim{
		Run:     run,
		Attempt: Attempt{ID: attemptID, RunID: run.ID, Number: attemptNumber, WorkerID: owner, State: RunPreparing},
		Lease: Lease{
			RunID: run.ID, Workspace: run.Workspace, Mode: mode, Owner: owner,
			AcquiredAt: now, HeartbeatAt: now, ExpiresAt: expiresAt,
		},
	}, nil
}

// PrepareClaim advances a leased run into preparation before the worker reads
// context, resolves a workspace, or constructs a harness invocation.
func (s *Store) PrepareClaim(ctx context.Context, claim Claim, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim preparation: %w", err)
	}
	defer tx.Rollback()
	if err := verifyLease(ctx, tx, claim, now); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, updated_at = ? WHERE id = ? AND state = ?
	`, RunPreparing, millis(now), claim.Run.ID, RunLeased)
	if err != nil {
		return fmt.Errorf("prepare claimed run: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: run %q is not leased", ErrConflict, claim.Run.ID)
	}
	if err := appendRunStateEvent(ctx, tx, claim.Run.ID, RunPreparing, claim.Attempt.ID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim preparation: %w", err)
	}
	return nil
}

// StartAttempt records the supervised process and advances a claimed run to running.
func (s *Store) StartAttempt(ctx context.Context, claim Claim, processID int, now time.Time) error {
	if processID <= 0 {
		return errors.New("process ID must be greater than zero")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attempt start: %w", err)
	}
	defer tx.Rollback()
	if err := verifyLease(ctx, tx, claim, now); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, updated_at = ? WHERE id = ? AND state = ?
	`, RunRunning, millis(now), claim.Run.ID, RunPreparing)
	if err != nil {
		return fmt.Errorf("start claimed run: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: run %q is not preparing", ErrConflict, claim.Run.ID)
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE run_attempts SET state = ?, process_id = ?, started_at = ?
		WHERE id = ? AND run_id = ? AND worker_id = ? AND state = ?
	`, RunRunning, processID, millis(now), claim.Attempt.ID, claim.Run.ID, claim.Lease.Owner, RunPreparing)
	if err != nil {
		return fmt.Errorf("start run attempt: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: attempt %q is not preparing", ErrConflict, claim.Attempt.ID)
	}
	if err := appendRunStateEvent(ctx, tx, claim.Run.ID, RunRunning, claim.Attempt.ID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attempt start: %w", err)
	}
	return nil
}

// HeartbeatLease extends a live claim owned by the same worker.
func (s *Store) HeartbeatLease(ctx context.Context, claim Claim, now time.Time, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("lease TTL must be greater than zero")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	result, err := s.db.ExecContext(ctx, `
		UPDATE run_leases SET heartbeat_at = ?, expires_at = ?
		WHERE run_id = ? AND owner = ? AND expires_at > ?
	`, millis(now), millis(now.Add(ttl)), claim.Run.ID, claim.Lease.Owner, millis(now))
	if err != nil {
		return fmt.Errorf("heartbeat run lease: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: run lease is missing, expired, or owned by another worker", ErrConflict)
	}
	return nil
}

// CompleteClaim stores the attempt result, transitions the run to a terminal
// state, and releases its workspace lease in one transaction.
func (s *Store) CompleteClaim(ctx context.Context, claim Claim, state RunState, conclusion string, result json.RawMessage, now time.Time) error {
	if state != RunSucceeded && state != RunFailed && state != RunCancelled && state != RunInterrupted {
		return fmt.Errorf("completion state %q is not terminal", state)
	}
	if len(result) != 0 && !json.Valid(result) {
		return errors.New("attempt result must be valid JSON")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin claim completion: %w", err)
	}
	defer tx.Rollback()
	if err := verifyLease(ctx, tx, claim, now); err != nil {
		return err
	}
	resultText := any(nil)
	if len(result) != 0 {
		resultText = string(result)
	}
	attemptQuery := `
		UPDATE run_attempts SET state = ?, finished_at = ?, result_json = ?
		WHERE id = ? AND run_id = ? AND worker_id = ? AND state = ?`
	attemptArgs := []any{state, millis(now), resultText, claim.Attempt.ID, claim.Run.ID, claim.Lease.Owner, RunRunning}
	if state != RunSucceeded {
		attemptQuery = `
			UPDATE run_attempts SET state = ?, finished_at = ?, result_json = ?
			WHERE id = ? AND run_id = ? AND worker_id = ? AND state IN (?, ?)`
		attemptArgs = []any{
			state, millis(now), resultText, claim.Attempt.ID, claim.Run.ID, claim.Lease.Owner,
			RunPreparing, RunRunning,
		}
	}
	updated, err := tx.ExecContext(ctx, attemptQuery, attemptArgs...)
	if err != nil {
		return fmt.Errorf("complete run attempt: %w", err)
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: attempt %q is not active", ErrConflict, claim.Attempt.ID)
	}
	runQuery := `
		UPDATE runs SET state = ?, conclusion = NULLIF(?, ''), updated_at = ?
		WHERE id = ? AND state = ?`
	runArgs := []any{state, conclusion, millis(now), claim.Run.ID, RunRunning}
	switch state {
	case RunFailed:
		runQuery = `
			UPDATE runs SET state = ?, conclusion = NULLIF(?, ''), updated_at = ?
			WHERE id = ? AND state IN (?, ?, ?)`
		runArgs = []any{state, conclusion, millis(now), claim.Run.ID, RunPreparing, RunRunning, RunWaitingForApproval}
	case RunCancelled, RunInterrupted:
		runQuery = `
			UPDATE runs SET state = ?, conclusion = NULLIF(?, ''), updated_at = ?
			WHERE id = ? AND state IN (?, ?, ?, ?)`
		runArgs = []any{state, conclusion, millis(now), claim.Run.ID, RunLeased, RunPreparing, RunRunning, RunWaitingForApproval}
	}
	updated, err = tx.ExecContext(ctx, runQuery, runArgs...)
	if err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: run %q is not active", ErrConflict, claim.Run.ID)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM run_leases WHERE run_id = ? AND owner = ?`, claim.Run.ID, claim.Lease.Owner,
	); err != nil {
		return fmt.Errorf("release run lease: %w", err)
	}
	if err := appendRunStateEvent(ctx, tx, claim.Run.ID, state, claim.Attempt.ID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim completion: %w", err)
	}
	return nil
}

// RecoverExpiredLeases reconciles work left active by a crashed worker.
// Leased-but-not-started work returns to pending; started work is interrupted.
func (s *Store) RecoverExpiredLeases(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin lease recovery: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT l.run_id, r.state
		FROM run_leases l JOIN runs r ON r.id = l.run_id
		WHERE l.expires_at <= ?
	`, millis(now))
	if err != nil {
		return 0, fmt.Errorf("list expired leases: %w", err)
	}
	type expired struct {
		runID string
		state RunState
	}
	var expiredRuns []expired
	for rows.Next() {
		var item expired
		if err := rows.Scan(&item.runID, &item.state); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan expired lease: %w", err)
		}
		expiredRuns = append(expiredRuns, item)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired leases: %w", err)
	}
	for _, item := range expiredRuns {
		next := RunInterrupted
		if item.state == RunLeased {
			next = RunPending
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE runs SET state = ?, updated_at = ? WHERE id = ?`, next, millis(now), item.runID,
		); err != nil {
			return 0, fmt.Errorf("recover run %q: %w", item.runID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE run_attempts SET state = ?, finished_at = ?
			WHERE run_id = ? AND state IN (?, ?)
		`, RunInterrupted, millis(now), item.runID, RunPreparing, RunRunning); err != nil {
			return 0, fmt.Errorf("interrupt attempt for %q: %w", item.runID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM run_leases WHERE run_id = ?`, item.runID); err != nil {
			return 0, fmt.Errorf("delete expired lease for %q: %w", item.runID, err)
		}
		if err := appendRunStateEvent(ctx, tx, item.runID, next, "", now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit lease recovery: %w", err)
	}
	return len(expiredRuns), nil
}

func verifyLease(ctx context.Context, tx *sql.Tx, claim Claim, now time.Time) error {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM run_leases
		WHERE run_id = ? AND owner = ? AND expires_at > ?
	`, claim.Run.ID, claim.Lease.Owner, millis(now)).Scan(&count); err != nil {
		return fmt.Errorf("verify run lease: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("%w: run lease is missing, expired, or owned by another worker", ErrConflict)
	}
	return nil
}

func leaseModeFor(permission Permission) LeaseMode {
	if permission == PermissionReadonly {
		return LeaseReadonly
	}
	return LeaseWrite
}
