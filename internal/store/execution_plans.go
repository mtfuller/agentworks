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

func (s *Store) SaveExecutionPlan(ctx context.Context, value ExecutionPlan) error {
	value.RunID = strings.TrimSpace(value.RunID)
	value.PlanDigest = strings.TrimSpace(value.PlanDigest)
	value.Runtime = strings.TrimSpace(value.Runtime)
	value.Network = strings.TrimSpace(value.Network)
	if value.RunID == "" || value.PlanDigest == "" || value.Runtime == "" || value.Network == "" || !json.Valid(value.Plan) {
		return errors.New("execution plan run, digest, runtime, network, and valid plan JSON are required")
	}
	if value.Provider != "host" && value.Provider != "container" && value.Provider != "remote" {
		return fmt.Errorf("execution provider %q is invalid", value.Provider)
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO run_execution_plans(run_id,plan_digest,provider,runtime,runtime_version,image_digest,network,plan_json,created_at)
		VALUES(?,?,?,?,NULLIF(?,''),NULLIF(?,''),?,?,?) ON CONFLICT(run_id) DO NOTHING
	`, value.RunID, value.PlanDigest, value.Provider, value.Runtime, value.RuntimeVersion, value.ImageDigest, value.Network, string(value.Plan), millis(value.CreatedAt))
	if err != nil {
		return fmt.Errorf("save execution plan: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 1 {
		return nil
	}
	stored, err := s.GetExecutionPlan(ctx, value.RunID)
	if err != nil {
		return err
	}
	if stored.PlanDigest != value.PlanDigest || stored.Provider != value.Provider || stored.Runtime != value.Runtime || stored.RuntimeVersion != value.RuntimeVersion || stored.ImageDigest != value.ImageDigest || stored.Network != value.Network || string(stored.Plan) != string(value.Plan) {
		return fmt.Errorf("%w: execution plan for run %q is immutable", ErrConflict, value.RunID)
	}
	return nil
}

func (s *Store) GetExecutionPlan(ctx context.Context, runID string) (ExecutionPlan, error) {
	var value ExecutionPlan
	var created int64
	var planJSON string
	err := s.db.QueryRowContext(ctx, `SELECT run_id,plan_digest,provider,runtime,COALESCE(runtime_version,''),COALESCE(image_digest,''),network,plan_json,created_at FROM run_execution_plans WHERE run_id=?`, runID).Scan(
		&value.RunID, &value.PlanDigest, &value.Provider, &value.Runtime, &value.RuntimeVersion, &value.ImageDigest, &value.Network, &planJSON, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionPlan{}, ErrNotFound
	}
	if err != nil {
		return ExecutionPlan{}, err
	}
	value.Plan = json.RawMessage(planJSON)
	value.CreatedAt = fromMillis(created)
	return value, nil
}
