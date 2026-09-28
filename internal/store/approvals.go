package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RequestApproval either reuses an identical approved run-only grant or
// atomically pauses the run and creates a pending approval request.
func (s *Store) RequestApproval(ctx context.Context, claim Claim, approval Approval, now time.Time) (Approval, bool, error) {
	approval.ID = strings.TrimSpace(approval.ID)
	approval.Kind = strings.TrimSpace(approval.Kind)
	approval.Summary = strings.TrimSpace(approval.Summary)
	if approval.ID == "" || approval.Kind == "" {
		return Approval{}, false, errors.New("approval ID and kind are required")
	}
	if len(approval.Scope) == 0 || !json.Valid(approval.Scope) {
		return Approval{}, false, errors.New("approval scope must be valid JSON")
	}
	approval.Scope = compactJSON(approval.Scope)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, false, fmt.Errorf("begin approval request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := verifyLease(ctx, tx, claim, now); err != nil {
		return Approval{}, false, err
	}
	approved, err := findApprovedScope(ctx, tx, claim.Run.ID, approval.Kind, approval.Scope)
	if err != nil {
		return Approval{}, false, err
	}
	if approved.ID != "" {
		if err := tx.Commit(); err != nil {
			return Approval{}, false, fmt.Errorf("commit reused approval: %w", err)
		}
		return approved, false, nil
	}
	var current RunState
	if err := tx.QueryRowContext(ctx, `SELECT state FROM runs WHERE id = ?`, claim.Run.ID).Scan(&current); err != nil {
		return Approval{}, false, fmt.Errorf("read run for approval: %w", err)
	}
	if current != RunPreparing && current != RunRunning {
		return Approval{}, false, fmt.Errorf("%w: run %q cannot request approval from %s", ErrConflict, claim.Run.ID, current)
	}
	approval.RunID = claim.Run.ID
	approval.AttemptID = claim.Attempt.ID
	approval.State = ApprovalPending
	approval.ResumeState = current
	approval.RequestedAt = now
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO approvals(id, run_id, attempt_id, kind, scope_json, state, requested_at, resume_state, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, approval.ID, approval.RunID, approval.AttemptID, approval.Kind, string(approval.Scope),
		approval.State, millis(now), approval.ResumeState, approval.Summary); err != nil {
		return Approval{}, false, fmt.Errorf("create approval: %w", err)
	}
	paused, err := tx.ExecContext(ctx, `UPDATE runs SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		RunWaitingForApproval, millis(now), approval.RunID, current)
	if err != nil {
		return Approval{}, false, fmt.Errorf("pause run for approval: %w", err)
	}
	if changed, _ := paused.RowsAffected(); changed != 1 {
		return Approval{}, false, fmt.Errorf("%w: run %q changed while requesting approval", ErrConflict, approval.RunID)
	}
	data, _ := json.Marshal(map[string]any{"approval_id": approval.ID, "kind": approval.Kind, "state": approval.State})
	if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{
		Topic: "approval.requested", EntityType: "run", EntityID: approval.RunID, Data: data, CreatedAt: now,
	}); err != nil {
		return Approval{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Approval{}, false, fmt.Errorf("commit approval request: %w", err)
	}
	return approval, true, nil
}

// DecideApproval records one human decision and resumes the owning harness. A
// denial is a tool-level result, not a run-level terminal state: the harness may
// recover, explain the denial, or request a different bounded action.
func (s *Store) DecideApproval(ctx context.Context, id string, decision ApprovalState, reason string, now time.Time) (Approval, error) {
	if decision != ApprovalApproved && decision != ApprovalDenied {
		return Approval{}, errors.New("approval decision must be approved or denied")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, fmt.Errorf("begin approval decision: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	approval, err := getApproval(ctx, tx, id)
	if err != nil {
		return Approval{}, err
	}
	if approval.State != ApprovalPending {
		return Approval{}, fmt.Errorf("%w: approval %q is already %s", ErrConflict, id, approval.State)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE approvals SET state = ?, decided_at = ?, decision_reason = NULLIF(?, '')
		WHERE id = ? AND state = ?
	`, decision, millis(now), reason, id, ApprovalPending); err != nil {
		return Approval{}, fmt.Errorf("decide approval: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE runs SET state = ?, updated_at = ? WHERE id = ? AND state = ?`,
		approval.ResumeState, millis(now), approval.RunID, RunWaitingForApproval)
	if err != nil {
		return Approval{}, fmt.Errorf("resume run after approval decision: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Approval{}, fmt.Errorf("%w: run %q is not waiting for approval", ErrConflict, approval.RunID)
	}
	data, _ := json.Marshal(map[string]any{"approval_id": approval.ID, "kind": approval.Kind, "state": decision})
	if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{
		Topic: "approval.decided", EntityType: "run", EntityID: approval.RunID, Data: data, CreatedAt: now,
	}); err != nil {
		return Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return Approval{}, fmt.Errorf("commit approval decision: %w", err)
	}
	approval.State = decision
	approval.DecisionReason = reason
	approval.DecidedAt = &now
	return approval, nil
}

func (s *Store) GetApproval(ctx context.Context, id string) (Approval, error) {
	return getApproval(ctx, s.db, id)
}

func (s *Store) CancelApproval(ctx context.Context, id, reason string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin approval cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	approval, err := getApproval(ctx, tx, id)
	if err != nil {
		return err
	}
	if approval.State != ApprovalPending {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE approvals SET state = ?, decided_at = ?, decision_reason = NULLIF(?, '')
		WHERE id = ? AND state = ?
	`, ApprovalCancelled, millis(now), reason, id, ApprovalPending); err != nil {
		return fmt.Errorf("cancel approval: %w", err)
	}
	data, _ := json.Marshal(map[string]any{"approval_id": approval.ID, "kind": approval.Kind, "state": ApprovalCancelled})
	if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{
		Topic: "approval.decided", EntityType: "run", EntityID: approval.RunID, Data: data, CreatedAt: now,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit approval cancellation: %w", err)
	}
	return nil
}

func (s *Store) ListApprovals(ctx context.Context, runID string) ([]Approval, error) {
	rows, err := s.db.QueryContext(ctx, approvalSelect+` WHERE run_id = ? ORDER BY requested_at, id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()
	approvals := make([]Approval, 0)
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	return approvals, nil
}

const approvalSelect = `SELECT id, run_id, COALESCE(attempt_id, ''), kind, COALESCE(summary, ''), scope_json, state,
	resume_state, requested_at, decided_at, COALESCE(decision_reason, '') FROM approvals`

func findApprovedScope(ctx context.Context, tx *sql.Tx, runID, kind string, scope json.RawMessage) (Approval, error) {
	approval, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+`
		WHERE run_id = ? AND kind = ? AND scope_json = ? AND state = ?
		ORDER BY decided_at DESC LIMIT 1`, runID, kind, string(scope), ApprovalApproved))
	if errors.Is(err, ErrNotFound) {
		return Approval{}, nil
	}
	return approval, err
}

func getApproval(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Approval, error) {
	return scanApproval(queryer.QueryRowContext(ctx, approvalSelect+` WHERE id = ?`, id))
}

func scanApproval(row rowScanner) (Approval, error) {
	var approval Approval
	var scope string
	var requestedAt int64
	var decidedAt sql.NullInt64
	if err := row.Scan(&approval.ID, &approval.RunID, &approval.AttemptID, &approval.Kind, &approval.Summary, &scope,
		&approval.State, &approval.ResumeState, &requestedAt, &decidedAt, &approval.DecisionReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("scan approval: %w", err)
	}
	approval.Scope = json.RawMessage(scope)
	approval.RequestedAt = fromMillis(requestedAt)
	approval.DecidedAt = optionalTime(decidedAt)
	return approval, nil
}

func compactJSON(value json.RawMessage) json.RawMessage {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return value
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return value
	}
	return canonical
}
