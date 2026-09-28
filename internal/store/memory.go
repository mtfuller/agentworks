package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (s *Store) CreateMemoryProposal(ctx context.Context, proposal MemoryProposal) error {
	if proposal.ID == "" || proposal.RunID == "" || proposal.TargetPath == "" || proposal.BaseSHA256 == "" {
		return errors.New("memory proposal identity, run, target, and base hash are required")
	}
	if proposal.Scope != "team" && proposal.Scope != "agent" {
		return errors.New("memory proposal scope must be team or agent")
	}
	if proposal.State == "" {
		proposal.State = MemoryProposalPending
	}
	if proposal.State != MemoryProposalPending {
		return errors.New("new memory proposal must be pending")
	}
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO memory_proposals(id,run_id,scope,target_path,base_sha256,before_markdown,after_markdown,diff,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, proposal.ID, proposal.RunID, proposal.Scope, proposal.TargetPath, proposal.BaseSHA256, proposal.BeforeMarkdown, proposal.AfterMarkdown, proposal.Diff, proposal.State, millis(proposal.CreatedAt))
	if err != nil {
		return fmt.Errorf("create memory proposal: %w", err)
	}
	return nil
}

func (s *Store) GetMemoryProposal(ctx context.Context, id string) (MemoryProposal, error) {
	var p MemoryProposal
	var created int64
	var decided sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,scope,target_path,base_sha256,before_markdown,after_markdown,diff,state,created_at,decided_at FROM memory_proposals WHERE id=?`, id).Scan(&p.ID, &p.RunID, &p.Scope, &p.TargetPath, &p.BaseSHA256, &p.BeforeMarkdown, &p.AfterMarkdown, &p.Diff, &p.State, &created, &decided)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt = fromMillis(created)
	p.DecidedAt = optionalTime(decided)
	return p, nil
}

func (s *Store) ListMemoryProposals(ctx context.Context, runID string) ([]MemoryProposal, error) {
	query := `SELECT id FROM memory_proposals`
	args := []any{}
	if runID != "" {
		query += ` WHERE run_id=?`
		args = append(args, runID)
	}
	query += ` ORDER BY created_at DESC,id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]MemoryProposal, 0, len(ids))
	for _, id := range ids {
		p, err := s.GetMemoryProposal(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// DecideMemoryProposal uses an expected pending state so filesystem conflict
// checks can never overwrite an independently decided proposal.
func (s *Store) DecideMemoryProposal(ctx context.Context, id string, state MemoryProposalState, details json.RawMessage, at time.Time) error {
	if state != MemoryProposalApproved && state != MemoryProposalRejected && state != MemoryProposalConflict {
		return errors.New("invalid memory proposal decision")
	}
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_proposals SET state=?,decided_at=? WHERE id=? AND state=?`, state, millis(at), id, MemoryProposalPending)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("%w: memory proposal is not pending", ErrConflict)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memory_audits(proposal_id,action,details_json,created_at) VALUES(?,?,?,?)`, id, state, string(details), millis(at)); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"state": state})
	if _, err = appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "memory.proposal", EntityType: "memory-proposal", EntityID: id, Data: data, CreatedAt: at}); err != nil {
		return err
	}
	return tx.Commit()
}
