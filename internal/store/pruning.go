package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PrunableRun struct {
	RunID      string    `json:"run_id"`
	AttemptIDs []string  `json:"attempt_ids"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) SetRunPinned(ctx context.Context, runID string, pinned bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE runs SET pinned=?,updated_at=? WHERE id=?`, pinned, millis(now), runID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPrunableRuns(ctx context.Context, limit int) ([]PrunableRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,created_at FROM runs
		WHERE pinned=0 AND detail_pruned_at IS NULL
		  AND state IN ('succeeded','failed','cancelled','interrupted')
		ORDER BY created_at,id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PrunableRun{}
	for rows.Next() {
		var value PrunableRun
		var created int64
		if err := rows.Scan(&value.RunID, &created); err != nil {
			return nil, err
		}
		value.CreatedAt = fromMillis(created)
		attemptRows, err := s.db.QueryContext(ctx, `SELECT id FROM run_attempts WHERE run_id=? ORDER BY number`, value.RunID)
		if err != nil {
			return nil, err
		}
		value.AttemptIDs = []string{}
		for attemptRows.Next() {
			var id string
			if err := attemptRows.Scan(&id); err != nil {
				attemptRows.Close()
				return nil, err
			}
			value.AttemptIDs = append(value.AttemptIDs, id)
		}
		if err := attemptRows.Close(); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// PruneRunDetail removes replayable raw detail while retaining the run,
// conclusions, outcomes, approvals, audit events, monitors, and plan.
func (s *Store) PruneRunDetail(ctx context.Context, runID string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pinned bool
	var state RunState
	var eventID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT pinned,state,event_record_id FROM runs WHERE id=?`, runID).Scan(&pinned, &state, &eventID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if pinned {
		return fmt.Errorf("%w: run %q is pinned", ErrConflict, runID)
	}
	if state != RunSucceeded && state != RunFailed && state != RunCancelled && state != RunInterrupted {
		return fmt.Errorf("%w: run %q is active", ErrConflict, runID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE run_attempts SET result_json=json_set(json_remove(COALESCE(result_json,'{}'),'$.log.path'),'$.log.pruned',json('true'),'$.log.bytes',0) WHERE run_id=?`, runID); err != nil {
		return err
	}
	if eventID.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE events SET data_json='{"pruned":true}',raw_data_json='{}' WHERE record_id=?`, eventID.String); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET detail_pruned_at=?,updated_at=? WHERE id=?`, millis(now), millis(now), runID); err != nil {
		return err
	}
	data := []byte(`{"detail":"pruned"}`)
	if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "run.pruned", EntityType: "run", EntityID: runID, Data: data, CreatedAt: now}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Compact(ctx context.Context) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}
