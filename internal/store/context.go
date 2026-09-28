package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SaveRunConclusion records the structured handoff for a terminal run. It is
// idempotent for identical data and refuses to silently replace a handoff.
func (s *Store) SaveRunConclusion(ctx context.Context, value StoredConclusion) error {
	value.RunID = strings.TrimSpace(value.RunID)
	if value.RunID == "" {
		return errors.New("conclusion run ID is required")
	}
	value.Conclusion.Normalize()
	data, err := json.Marshal(value.Conclusion)
	if err != nil {
		return fmt.Errorf("encode run conclusion: %w", err)
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO run_conclusions(run_id, conclusion_json, private_memory, structured, created_at)
		SELECT ?, ?, ?, ?, ? WHERE EXISTS (
			SELECT 1 FROM runs WHERE id = ? AND state IN ('succeeded','failed','cancelled','interrupted')
		)
		ON CONFLICT(run_id) DO NOTHING
	`, value.RunID, string(data), value.PrivateMemory, value.Structured, millis(value.CreatedAt), value.RunID)
	if err != nil {
		return fmt.Errorf("save run conclusion: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		return nil
	}
	existing, err := s.GetRunConclusion(ctx, value.RunID)
	if err != nil {
		return fmt.Errorf("%w: run is not terminal", ErrConflict)
	}
	existingData, _ := json.Marshal(existing.Conclusion)
	if string(existingData) != string(data) || existing.PrivateMemory != value.PrivateMemory || existing.Structured != value.Structured {
		return fmt.Errorf("%w: run conclusion already exists", ErrConflict)
	}
	return nil
}

func (s *Store) GetRunConclusion(ctx context.Context, runID string) (StoredConclusion, error) {
	var value StoredConclusion
	var data string
	var createdAt int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT run_id, conclusion_json, private_memory, structured, created_at
		FROM run_conclusions WHERE run_id = ?
	`, runID).Scan(&value.RunID, &data, &value.PrivateMemory, &value.Structured, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StoredConclusion{}, ErrNotFound
		}
		return StoredConclusion{}, fmt.Errorf("load run conclusion: %w", err)
	}
	if err := json.Unmarshal([]byte(data), &value.Conclusion); err != nil {
		return StoredConclusion{}, fmt.Errorf("decode run conclusion: %w", err)
	}
	value.Conclusion.Normalize()
	value.CreatedAt = fromMillis(createdAt)
	return value, nil
}

func (s *Store) UpsertWorkItem(ctx context.Context, item WorkItem) (WorkItem, error) {
	item.ID, item.Kind, item.ExternalKey = strings.TrimSpace(item.ID), strings.TrimSpace(item.Kind), strings.TrimSpace(item.ExternalKey)
	if item.ID == "" || item.Kind == "" || item.ExternalKey == "" {
		return WorkItem{}, errors.New("work item ID, kind, and external key are required")
	}
	if len(item.Data) == 0 {
		item.Data = json.RawMessage(`{}`)
	}
	if !json.Valid(item.Data) {
		return WorkItem{}, errors.New("work item data must be valid JSON")
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO work_items(id, kind, external_key, title, state, data_json, created_at, updated_at)
		VALUES (?, ?, ?, NULLIF(?,''), NULLIF(?,''), ?, ?, ?)
		ON CONFLICT(kind, external_key) DO UPDATE SET title=excluded.title, state=excluded.state,
			data_json=excluded.data_json, updated_at=excluded.updated_at
	`, item.ID, item.Kind, item.ExternalKey, item.Title, item.State, string(item.Data), millis(item.CreatedAt), millis(item.UpdatedAt))
	if err != nil {
		return WorkItem{}, fmt.Errorf("upsert work item: %w", err)
	}
	return s.workItemByIdentity(ctx, item.Kind, item.ExternalKey)
}

func (s *Store) workItemByIdentity(ctx context.Context, kind, key string) (WorkItem, error) {
	var item WorkItem
	var data string
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, external_key, COALESCE(title,''), COALESCE(state,''), data_json, created_at, updated_at FROM work_items WHERE kind=? AND external_key=?`, kind, key).
		Scan(&item.ID, &item.Kind, &item.ExternalKey, &item.Title, &item.State, &data, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkItem{}, ErrNotFound
	}
	if err != nil {
		return WorkItem{}, fmt.Errorf("load work item: %w", err)
	}
	item.Data, item.CreatedAt, item.UpdatedAt = json.RawMessage(data), fromMillis(createdAt), fromMillis(updatedAt)
	return item, nil
}

func (s *Store) RecordOutcome(ctx context.Context, outcome Outcome) (Outcome, error) {
	outcome.ID, outcome.Type = strings.TrimSpace(outcome.ID), strings.TrimSpace(outcome.Type)
	if outcome.ID == "" || outcome.Type == "" {
		return Outcome{}, errors.New("outcome ID and type are required")
	}
	if outcome.OccurredAt.IsZero() {
		outcome.OccurredAt = time.Now().UTC()
	}
	if len(outcome.Data) == 0 {
		outcome.Data = json.RawMessage(`{}`)
	}
	if !json.Valid(outcome.Data) {
		return Outcome{}, errors.New("outcome data must be valid JSON")
	}
	if outcome.WorkItemID == "" && outcome.RunID != "" {
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(e.work_item_id,'') FROM runs r LEFT JOIN events e ON e.record_id=r.event_record_id WHERE r.id=?`, outcome.RunID).Scan(&outcome.WorkItemID)
	} else if outcome.WorkItemID != "" && outcome.RunID != "" {
		var attached string
		err := s.db.QueryRowContext(ctx, `SELECT COALESCE(e.work_item_id,'') FROM runs r JOIN events e ON e.record_id=r.event_record_id WHERE r.id=?`, outcome.RunID).Scan(&attached)
		if err != nil {
			return Outcome{}, fmt.Errorf("correlate outcome run: %w", err)
		}
		if attached != "" && attached != outcome.WorkItemID {
			return Outcome{}, fmt.Errorf("%w: run is attached to work item %q", ErrConflict, attached)
		}
		if attached == "" {
			if _, err := s.db.ExecContext(ctx, `UPDATE events SET work_item_id=? WHERE record_id=(SELECT event_record_id FROM runs WHERE id=?) AND work_item_id IS NULL`, outcome.WorkItemID, outcome.RunID); err != nil {
				return Outcome{}, fmt.Errorf("attach run work item: %w", err)
			}
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO outcomes(id,type,subject,event_record_id,run_id,work_item_id,occurred_at,data_json)
		VALUES (?, ?, NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), ?, ?)`, outcome.ID, outcome.Type, outcome.Subject,
		outcome.EventRecordID, outcome.RunID, outcome.WorkItemID, millis(outcome.OccurredAt), string(outcome.Data))
	if err != nil {
		return Outcome{}, fmt.Errorf("record outcome: %w", err)
	}
	return outcome, nil
}

func (s *Store) ListOutcomes(ctx context.Context, workItemID string, limit int) ([]Outcome, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	query := `SELECT id,type,COALESCE(subject,''),COALESCE(event_record_id,''),COALESCE(run_id,''),COALESCE(work_item_id,''),occurred_at,data_json FROM outcomes`
	args := []any{}
	if workItemID != "" {
		query += ` WHERE work_item_id=?`
		args = append(args, workItemID)
	}
	query += ` ORDER BY occurred_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list outcomes: %w", err)
	}
	defer rows.Close()
	values := []Outcome{}
	for rows.Next() {
		var v Outcome
		var at int64
		var data string
		if err := rows.Scan(&v.ID, &v.Type, &v.Subject, &v.EventRecordID, &v.RunID, &v.WorkItemID, &at, &data); err != nil {
			return nil, err
		}
		v.OccurredAt = fromMillis(at)
		v.Data = json.RawMessage(data)
		values = append(values, v)
	}
	return values, rows.Err()
}

func (s *Store) WorkItemTimeline(ctx context.Context, workItemID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'event', record_id, type, status, COALESCE(subject,''), occurred_at, data_json FROM events WHERE work_item_id=?
		UNION ALL SELECT 'run', r.id, r.agent, r.state, COALESCE(rc.conclusion_json,r.conclusion,''), r.created_at,
			json_object('private_memory',COALESCE(rc.private_memory,''))
			FROM runs r LEFT JOIN run_conclusions rc ON rc.run_id=r.id
			WHERE r.event_record_id IN (SELECT record_id FROM events WHERE work_item_id=?)
		UNION ALL SELECT 'outcome', id, type, '', COALESCE(subject,''), occurred_at, data_json FROM outcomes WHERE work_item_id=?
	`, workItemID, workItemID, workItemID)
	if err != nil {
		return nil, fmt.Errorf("load work item timeline: %w", err)
	}
	defer rows.Close()
	values := []TimelineEntry{}
	for rows.Next() {
		var v TimelineEntry
		var at int64
		var data string
		if err := rows.Scan(&v.Kind, &v.ID, &v.Type, &v.State, &v.Summary, &at, &data); err != nil {
			return nil, err
		}
		v.OccurredAt = fromMillis(at)
		v.Data = json.RawMessage(data)
		values = append(values, v)
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].OccurredAt.Equal(values[j].OccurredAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].OccurredAt.Before(values[j].OccurredAt)
	})
	return values, rows.Err()
}

// ContextEntries returns prior durable handoffs and outcomes for the current
// work item. It deliberately never returns raw transcript text.
func (s *Store) ContextEntries(ctx context.Context, run Run) ([]TimelineEntry, error) {
	if run.EventRecordID == "" {
		return []TimelineEntry{}, nil
	}
	var workItemID string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(work_item_id,'') FROM events WHERE record_id=?`, run.EventRecordID).Scan(&workItemID)
	if err != nil || workItemID == "" {
		return []TimelineEntry{}, err
	}
	entries, err := s.WorkItemTimeline(ctx, workItemID)
	if err != nil {
		return nil, err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.ID != run.ID && (entry.Kind == "outcome" || entry.Kind == "run") {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}

func (s *Store) PutMonitorState(ctx context.Context, state MonitorState) error {
	if len(state.Window) == 0 {
		state.Window = json.RawMessage(`{}`)
	}
	if len(state.Details) == 0 {
		state.Details = json.RawMessage(`{}`)
	}
	if !json.Valid(state.Window) || !json.Valid(state.Details) {
		return errors.New("monitor window and details must be valid JSON")
	}
	if state.EvaluatedAt.IsZero() {
		state.EvaluatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO monitor_states(monitor_id,revision,state,window_json,details_json,evaluated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(monitor_id) DO UPDATE SET revision=excluded.revision,state=excluded.state,window_json=excluded.window_json,details_json=excluded.details_json,evaluated_at=excluded.evaluated_at`, state.MonitorID, state.Revision, state.State, string(state.Window), string(state.Details), millis(state.EvaluatedAt))
	return err
}

func (s *Store) ListMonitorStates(ctx context.Context) ([]MonitorState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT monitor_id,revision,state,window_json,details_json,evaluated_at FROM monitor_states ORDER BY monitor_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonitorState{}
	for rows.Next() {
		var v MonitorState
		var w, d string
		var at int64
		if err := rows.Scan(&v.MonitorID, &v.Revision, &v.State, &w, &d, &at); err != nil {
			return nil, err
		}
		v.Window = json.RawMessage(w)
		v.Details = json.RawMessage(d)
		v.EvaluatedAt = fromMillis(at)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ReadRuntimeMetrics(ctx context.Context, now time.Time) (RuntimeMetrics, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var metrics RuntimeMetrics
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MIN(created_at) FROM runs WHERE state IN ('pending','retry-scheduled')`).Scan(&metrics.PendingRuns, &oldest); err != nil {
		return metrics, err
	}
	if oldest.Valid {
		metrics.OldestQueueDelay = now.Sub(fromMillis(oldest.Int64))
		if metrics.OldestQueueDelay < 0 {
			metrics.OldestQueueDelay = 0
		}
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN number>1 THEN 1 ELSE 0 END),0) FROM run_attempts`).Scan(&metrics.Attempts, &metrics.RetryAttempts); err != nil {
		return metrics, err
	}
	var totalDuration int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(finished_at-started_at),0) FROM run_attempts WHERE started_at IS NOT NULL AND finished_at IS NOT NULL`).Scan(&metrics.CompletedRuns, &totalDuration); err != nil {
		return metrics, err
	}
	if metrics.CompletedRuns > 0 {
		metrics.AverageDuration = time.Duration(totalDuration/int64(metrics.CompletedRuns)) * time.Millisecond
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN c.last_success_at IS NULL OR c.last_success_at<? THEN 1 ELSE 0 END),0) FROM sources s LEFT JOIN source_cursors c ON c.source_id=s.id WHERE s.definition_enabled=1 AND s.paused=0`, millis(now.Add(-time.Hour))).Scan(&metrics.ConfiguredSources, &metrics.StaleSources); err != nil {
		return metrics, err
	}
	return metrics, nil
}
