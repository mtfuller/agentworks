package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// NextRetryDue returns the earliest pending run retry timer. It lets the
// in-process runtime sleep until real work is due instead of polling rapidly.
func (s *Store) NextRetryDue(ctx context.Context) (time.Time, bool, error) {
	var due sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT MIN(due_at) FROM timers WHERE kind = 'run-retry' AND state = 'pending'
	`).Scan(&due); err != nil {
		return time.Time{}, false, fmt.Errorf("read next retry timer: %w", err)
	}
	if !due.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(due.Int64).UTC(), true, nil
}

// ScheduleClaimRetry closes the current attempt, releases its lease, and
// creates one durable timer in the same transaction.
func (s *Store) ScheduleClaimRetry(ctx context.Context, claim Claim, dueAt time.Time, reason string, result json.RawMessage, now time.Time) error {
	if dueAt.IsZero() {
		return errors.New("retry due time is required")
	}
	if len(result) != 0 && !json.Valid(result) {
		return errors.New("retry result must be valid JSON")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	dueAt = dueAt.UTC()
	if dueAt.Before(now) {
		dueAt = now
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retry scheduling: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := verifyLease(ctx, tx, claim, now); err != nil {
		return err
	}
	resultText := any(nil)
	if len(result) != 0 {
		resultText = string(result)
	}
	updated, err := tx.ExecContext(ctx, `
		UPDATE run_attempts SET state = ?, finished_at = ?, result_json = ?
		WHERE id = ? AND run_id = ? AND worker_id = ? AND state = ?
	`, RunFailed, millis(now), resultText, claim.Attempt.ID, claim.Run.ID, claim.Lease.Owner, RunRunning)
	if err != nil {
		return fmt.Errorf("finish retryable attempt: %w", err)
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: attempt %q is not running", ErrConflict, claim.Attempt.ID)
	}
	updated, err = tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, conclusion = NULLIF(?, ''), updated_at = ?
		WHERE id = ? AND state = ?
	`, RunRetryScheduled, reason, millis(now), claim.Run.ID, RunRunning)
	if err != nil {
		return fmt.Errorf("mark run retry scheduled: %w", err)
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: run %q is not running", ErrConflict, claim.Run.ID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM run_leases WHERE run_id = ? AND owner = ?`, claim.Run.ID, claim.Lease.Owner); err != nil {
		return fmt.Errorf("release retrying run lease: %w", err)
	}
	timerID := fmt.Sprintf("retry:%s:%d", claim.Run.ID, claim.Attempt.Number)
	payload, _ := json.Marshal(map[string]any{"run_id": claim.Run.ID, "attempt": claim.Attempt.Number})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO timers(id, idempotency_key, kind, due_at, state, payload_json, created_at, updated_at)
		VALUES (?, ?, 'run-retry', ?, 'pending', ?, ?, ?)
	`, timerID, timerID, millis(dueAt), string(payload), millis(now), millis(now)); err != nil {
		return fmt.Errorf("create retry timer: %w", err)
	}
	if err := appendRunStateEvent(ctx, tx, claim.Run.ID, RunRetryScheduled, claim.Attempt.ID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retry scheduling: %w", err)
	}
	return nil
}

// ActivateDueRetries moves due retry-scheduled runs back to pending. A timer
// and its run transition are committed together, making restart catch-up safe.
func (s *Store) ActivateDueRetries(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin retry activation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		SELECT id, json_extract(payload_json, '$.run_id')
		FROM timers WHERE kind = 'run-retry' AND state = 'pending' AND due_at <= ?
		ORDER BY due_at, id LIMIT ?
	`, millis(now), limit)
	if err != nil {
		return 0, fmt.Errorf("list due retries: %w", err)
	}
	type dueRetry struct{ timerID, runID string }
	var due []dueRetry
	for rows.Next() {
		var item dueRetry
		if err := rows.Scan(&item.timerID, &item.runID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan due retry: %w", err)
		}
		due = append(due, item)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close due retries: %w", err)
	}
	activated := 0
	for _, item := range due {
		updated, err := tx.ExecContext(ctx, `
			UPDATE runs SET state = ?, conclusion = NULL, updated_at = ?
			WHERE id = ? AND state = ?
		`, RunPending, millis(now), item.runID, RunRetryScheduled)
		if err != nil {
			return 0, fmt.Errorf("activate retry run: %w", err)
		}
		changed, _ := updated.RowsAffected()
		timerState := "cancelled"
		if changed == 1 {
			timerState = "completed"
			activated++
			if err := appendRunStateEvent(ctx, tx, item.runID, RunPending, "", now); err != nil {
				return 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE timers SET state = ?, updated_at = ? WHERE id = ? AND state = 'pending'`, timerState, millis(now), item.timerID); err != nil {
			return 0, fmt.Errorf("complete retry timer: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit retry activation: %w", err)
	}
	return activated, nil
}
