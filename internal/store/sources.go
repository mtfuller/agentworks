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

func (s *Store) SyncSource(ctx context.Context, value SourceStatus) error {
	value.ID, value.Kind, value.ConfigRevision = strings.TrimSpace(value.ID), strings.TrimSpace(value.Kind), strings.TrimSpace(value.ConfigRevision)
	if value.ID == "" || value.Kind == "" || value.ConfigRevision == "" {
		return errors.New("source ID, kind, and config revision are required")
	}
	now := value.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	created := value.CreatedAt
	if created.IsZero() {
		created = now
	}
	if value.NextPollAt.IsZero() {
		value.NextPollAt = now
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sources(id,kind,config_revision,state,definition_enabled,paused,next_poll_at,failure_count,created_at,updated_at)
		VALUES(?,?,?,'unknown',?,0,?,0,?,?)
		ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,config_revision=excluded.config_revision,
			definition_enabled=excluded.definition_enabled,updated_at=excluded.updated_at
	`, value.ID, value.Kind, value.ConfigRevision, value.DefinitionEnabled, millis(value.NextPollAt), millis(created), millis(now))
	if err != nil {
		return fmt.Errorf("sync source: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO source_cursors(source_id,cursor,updated_at) VALUES(?,NULL,?) ON CONFLICT(source_id) DO NOTHING`, value.ID, millis(now)); err != nil {
		return fmt.Errorf("sync source cursor: %w", err)
	}
	return tx.Commit()
}

func (s *Store) SetSourcePaused(ctx context.Context, id string, paused bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sources SET paused=?,updated_at=? WHERE id=?`, paused, millis(now), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DisableConnectorSourcesExcept(ctx context.Context, active map[string]bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sources WHERE kind IN ('jira','github')`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if !active[id] {
			if _, err := s.db.ExecContext(ctx, `UPDATE sources SET definition_enabled=0,updated_at=? WHERE id=?`, millis(now), id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Source(ctx context.Context, id string) (SourceStatus, error) {
	return scanSource(s.db.QueryRowContext(ctx, sourceSelect+` WHERE s.id=?`, id))
}

func (s *Store) ListSources(ctx context.Context) ([]SourceStatus, error) {
	rows, err := s.db.QueryContext(ctx, sourceSelect+` ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []SourceStatus{}
	for rows.Next() {
		value, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) DueSources(ctx context.Context, now time.Time, limit int) ([]SourceStatus, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, sourceSelect+` WHERE s.definition_enabled=1 AND s.paused=0 AND COALESCE(s.next_poll_at,0)<=? ORDER BY s.next_poll_at,s.id LIMIT ?`, millis(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []SourceStatus{}
	for rows.Next() {
		value, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) NextSourceDue(ctx context.Context) (time.Time, bool, error) {
	var value sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(next_poll_at) FROM sources WHERE definition_enabled=1 AND paused=0`).Scan(&value); err != nil {
		return time.Time{}, false, err
	}
	if !value.Valid {
		return time.Time{}, false, nil
	}
	return fromMillis(value.Int64), true, nil
}

func (s *Store) RecordSourceFailure(ctx context.Context, id, message string, next time.Time, rateLimitReset time.Time, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if next.IsZero() {
		next = now.Add(time.Minute)
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sources SET state=CASE WHEN failure_count>=2 THEN 'failed' ELSE 'degraded' END,last_error=?,last_poll_at=?,next_poll_at=?,rate_limit_reset_at=NULLIF(?,0),failure_count=failure_count+1,updated_at=? WHERE id=?`, message, millis(now), millis(next), optionalMillis(rateLimitReset), millis(now), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

// StartSourcePoll records a poll before contacting an external service. A
// process interruption therefore remains visible as a running attempt instead
// of disappearing from diagnostics.
func (s *Store) StartSourcePoll(ctx context.Context, attempt SourcePollAttempt) error {
	attempt.ID, attempt.SourceID = strings.TrimSpace(attempt.ID), strings.TrimSpace(attempt.SourceID)
	if attempt.ID == "" || attempt.SourceID == "" || attempt.StartedAt.IsZero() {
		return errors.New("source poll attempt id, source, and start time are required")
	}
	if attempt.State == "" {
		attempt.State = "running"
	}
	if attempt.State != "running" {
		return errors.New("source poll attempt must start running")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO source_poll_attempts(id,source_id,state,cursor_before,started_at)
		VALUES(?,?,?,NULLIF(?,''),?)`, attempt.ID, attempt.SourceID, attempt.State, attempt.CursorBefore, millis(attempt.StartedAt))
	if err != nil {
		return fmt.Errorf("start source poll attempt: %w", err)
	}
	return nil
}

// CompleteSourcePoll records routing results after a successful atomic poll
// commit. Only opaque event and run identifiers are retained.
func (s *Store) CompleteSourcePoll(ctx context.Context, attempt SourcePollAttempt) (SourcePollAttempt, error) {
	if strings.TrimSpace(attempt.ID) == "" || attempt.FinishedAt.IsZero() {
		return SourcePollAttempt{}, errors.New("source poll attempt id and finish time are required")
	}
	attempt.EventRecordIDs = normalizedIDs(attempt.EventRecordIDs)
	attempt.RunIDs = normalizedIDs(attempt.RunIDs)
	events, _ := json.Marshal(attempt.EventRecordIDs)
	runs, _ := json.Marshal(attempt.RunIDs)
	query := `UPDATE source_poll_attempts SET state='succeeded',routed_runs=?,run_ids_json=?,finished_at=? WHERE id=? AND state IN ('running','committed')`
	args := []any{attempt.RoutedRuns, string(runs), millis(attempt.FinishedAt), attempt.ID}
	if len(attempt.EventRecordIDs) > 0 {
		query = `UPDATE source_poll_attempts SET state='succeeded',routed_runs=?,event_ids_json=?,run_ids_json=?,finished_at=? WHERE id=? AND state IN ('running','committed')`
		args = []any{attempt.RoutedRuns, string(events), string(runs), millis(attempt.FinishedAt), attempt.ID}
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return SourcePollAttempt{}, fmt.Errorf("complete source poll attempt: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return SourcePollAttempt{}, ErrNotFound
	}
	return s.SourcePollAttempt(ctx, attempt.ID)
}

// FailSourcePoll atomically retains a safe failure classification and updates
// source backoff state. It must be used instead of losing connector failures
// in a transient log.
func (s *Store) FailSourcePoll(ctx context.Context, attempt SourcePollAttempt) (SourcePollAttempt, error) {
	if strings.TrimSpace(attempt.ID) == "" || strings.TrimSpace(attempt.SourceID) == "" || attempt.FinishedAt.IsZero() {
		return SourcePollAttempt{}, errors.New("source poll failure requires attempt id, source, and finish time")
	}
	if len(attempt.ErrorCode) > 128 {
		attempt.ErrorCode = attempt.ErrorCode[:128]
	}
	if len(attempt.ErrorMessage) > 1024 {
		attempt.ErrorMessage = attempt.ErrorMessage[:1024]
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SourcePollAttempt{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE source_poll_attempts SET state='failed',error_code=NULLIF(?,''),error_message=NULLIF(?,''),finished_at=?,next_poll_at=?,rate_limit_reset_at=NULLIF(?,0) WHERE id=? AND source_id=? AND state='running'`, attempt.ErrorCode, attempt.ErrorMessage, millis(attempt.FinishedAt), millis(attempt.NextPollAt), optionalMillis(attempt.RateLimitResetAt), attempt.ID, attempt.SourceID)
	if err != nil {
		return SourcePollAttempt{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return SourcePollAttempt{}, ErrNotFound
	}
	result, err = tx.ExecContext(ctx, `UPDATE sources SET state=CASE WHEN failure_count>=2 THEN 'failed' ELSE 'degraded' END,last_error=?,last_poll_at=?,next_poll_at=?,rate_limit_reset_at=NULLIF(?,0),failure_count=failure_count+1,updated_at=? WHERE id=?`, attempt.ErrorMessage, millis(attempt.FinishedAt), millis(attempt.NextPollAt), optionalMillis(attempt.RateLimitResetAt), millis(attempt.FinishedAt), attempt.SourceID)
	if err != nil {
		return SourcePollAttempt{}, err
	}
	changed, _ = result.RowsAffected()
	if changed != 1 {
		return SourcePollAttempt{}, ErrNotFound
	}
	notice, _ := json.Marshal(map[string]any{"state": "failed", "code": attempt.ErrorCode})
	if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "source.poll", EntityType: "source", EntityID: attempt.SourceID, Data: notice, CreatedAt: attempt.FinishedAt}); err != nil {
		return SourcePollAttempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourcePollAttempt{}, err
	}
	return s.SourcePollAttempt(ctx, attempt.ID)
}

func (s *Store) SourcePollAttempt(ctx context.Context, id string) (SourcePollAttempt, error) {
	return scanSourcePollAttempt(s.db.QueryRowContext(ctx, sourcePollAttemptSelect+` WHERE id=?`, id))
}

func (s *Store) ListSourcePollAttempts(ctx context.Context, sourceID string, limit int) ([]SourcePollAttempt, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, sourcePollAttemptSelect+` WHERE source_id=? ORDER BY started_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SourcePollAttempt{}
	for rows.Next() {
		attempt, err := scanSourcePollAttempt(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, attempt)
	}
	return result, rows.Err()
}

func (s *Store) RecordSourceTest(ctx context.Context, id string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sources SET state='healthy',last_error=NULL,failure_count=0,updated_at=? WHERE id=?`, millis(now), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CommitPoll(ctx context.Context, commit PollCommit) (PollCommitResult, error) {
	commit.SourceID = strings.TrimSpace(commit.SourceID)
	if commit.SourceID == "" || commit.PolledAt.IsZero() || commit.NextPollAt.IsZero() {
		return PollCommitResult{}, errors.New("poll source, poll time, and next poll time are required")
	}
	for index := range commit.Events {
		if err := normalizeEvent(&commit.Events[index]); err != nil {
			return PollCommitResult{}, err
		}
	}
	for _, item := range commit.WorkItems {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.ExternalKey) == "" || (len(item.Data) != 0 && !json.Valid(item.Data)) {
			return PollCommitResult{}, errors.New("poll work item is invalid")
		}
	}
	for _, outcome := range commit.Outcomes {
		if strings.TrimSpace(outcome.ID) == "" || strings.TrimSpace(outcome.Type) == "" || (len(outcome.Data) != 0 && !json.Valid(outcome.Data)) {
			return PollCommitResult{}, errors.New("poll outcome is invalid")
		}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PollCommitResult{}, err
	}
	defer tx.Rollback()
	for _, item := range commit.WorkItems {
		data := item.Data
		if len(data) == 0 {
			data = json.RawMessage(`{}`)
		}
		created, updated := item.CreatedAt, item.UpdatedAt
		if created.IsZero() {
			created = commit.PolledAt
		}
		if updated.IsZero() {
			updated = commit.PolledAt
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO work_items(id,kind,external_key,title,state,data_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(kind,external_key) DO UPDATE SET title=COALESCE(NULLIF(excluded.title,''),work_items.title),state=COALESCE(NULLIF(excluded.state,''),work_items.state),data_json=json_patch(work_items.data_json,excluded.data_json),updated_at=excluded.updated_at`, item.ID, item.Kind, item.ExternalKey, item.Title, item.State, string(data), millis(created), millis(updated)); err != nil {
			return PollCommitResult{}, fmt.Errorf("commit poll work item: %w", err)
		}
	}
	result := PollCommitResult{EventRecordIDs: []string{}}
	for _, event := range commit.Events {
		insert, err := tx.ExecContext(ctx, `INSERT INTO events(record_id,source,external_id,type,subject,occurred_at,received_at,data_json,raw_data_json,status,work_item_id) VALUES(?,?,?,?,NULLIF(?,''),?,?,?,?,?,NULLIF(?,'')) ON CONFLICT(source,external_id) DO NOTHING`, event.RecordID, event.Source, event.ExternalID, event.Type, event.Subject, millis(event.OccurredAt), millis(event.ReceivedAt), string(event.Data), string(event.RawData), event.Status, event.WorkItemID)
		if err != nil {
			return PollCommitResult{}, fmt.Errorf("commit poll event: %w", err)
		}
		rows, _ := insert.RowsAffected()
		if rows == 1 {
			result.EventsInserted++
			data, _ := json.Marshal(map[string]any{"status": event.Status})
			if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "event.ingested", EntityType: "event", EntityID: event.RecordID, Data: data, CreatedAt: event.ReceivedAt}); err != nil {
				return PollCommitResult{}, err
			}
		}
		result.EventRecordIDs = append(result.EventRecordIDs, event.RecordID)
	}
	for _, outcome := range commit.Outcomes {
		data := outcome.Data
		if len(data) == 0 {
			data = json.RawMessage(`{}`)
		}
		occurred := outcome.OccurredAt
		if occurred.IsZero() {
			occurred = commit.PolledAt
		}
		insert, err := tx.ExecContext(ctx, `INSERT INTO outcomes(id,type,subject,event_record_id,run_id,work_item_id,occurred_at,data_json) VALUES(?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?,?) ON CONFLICT(id) DO NOTHING`, outcome.ID, outcome.Type, outcome.Subject, outcome.EventRecordID, outcome.RunID, outcome.WorkItemID, millis(occurred), string(data))
		if err != nil {
			return PollCommitResult{}, fmt.Errorf("commit poll outcome: %w", err)
		}
		rows, _ := insert.RowsAffected()
		result.OutcomesInserted += int(rows)
	}
	cursorUpdate, err := tx.ExecContext(ctx, `UPDATE source_cursors SET cursor=NULLIF(?,''),last_success_at=?,updated_at=? WHERE source_id=? AND COALESCE(cursor,'')=?`, commit.Cursor, millis(commit.PolledAt), millis(commit.PolledAt), commit.SourceID, commit.ExpectedCursor)
	if err != nil {
		return PollCommitResult{}, fmt.Errorf("advance source cursor: %w", err)
	}
	changedCursor, _ := cursorUpdate.RowsAffected()
	if changedCursor != 1 {
		return PollCommitResult{}, fmt.Errorf("%w: source cursor changed during poll", ErrConflict)
	}
	update, err := tx.ExecContext(ctx, `UPDATE sources SET state='healthy',last_error=NULL,last_poll_at=?,next_poll_at=?,rate_limit_reset_at=NULLIF(?,0),failure_count=0,updated_at=? WHERE id=?`, millis(commit.PolledAt), millis(commit.NextPollAt), optionalMillis(commit.RateLimitResetAt), millis(commit.PolledAt), commit.SourceID)
	if err != nil {
		return PollCommitResult{}, err
	}
	changed, _ := update.RowsAffected()
	if changed != 1 {
		return PollCommitResult{}, ErrNotFound
	}
	if commit.AttemptID != "" {
		ids := make([]string, 0, len(commit.Events))
		for _, event := range commit.Events {
			ids = append(ids, event.RecordID)
		}
		ids = normalizedIDs(ids)
		encoded, _ := json.Marshal(ids)
		attemptUpdate, err := tx.ExecContext(ctx, `UPDATE source_poll_attempts SET state='committed',cursor_after=NULLIF(?,''),events_observed=?,events_inserted=?,outcomes_observed=?,outcomes_inserted=?,event_ids_json=?,next_poll_at=?,rate_limit_reset_at=NULLIF(?,0),finished_at=? WHERE id=? AND source_id=? AND state='running'`, commit.Cursor, len(commit.Events), result.EventsInserted, len(commit.Outcomes), result.OutcomesInserted, string(encoded), millis(commit.NextPollAt), optionalMillis(commit.RateLimitResetAt), millis(commit.PolledAt), commit.AttemptID, commit.SourceID)
		if err != nil {
			return PollCommitResult{}, err
		}
		attemptChanged, _ := attemptUpdate.RowsAffected()
		if attemptChanged != 1 {
			return PollCommitResult{}, ErrNotFound
		}
	}
	notice, _ := json.Marshal(map[string]any{"events": result.EventsInserted, "outcomes": result.OutcomesInserted, "state": "healthy"})
	if _, err := appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "source.poll", EntityType: "source", EntityID: commit.SourceID, Data: notice, CreatedAt: commit.PolledAt}); err != nil {
		return PollCommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PollCommitResult{}, err
	}
	return result, nil
}

const sourceSelect = `SELECT s.id,s.kind,s.config_revision,s.state,s.definition_enabled,s.paused,COALESCE(s.last_error,''),COALESCE(c.cursor,''),s.last_poll_at,c.last_success_at,s.next_poll_at,s.rate_limit_reset_at,s.failure_count,s.created_at,s.updated_at FROM sources s LEFT JOIN source_cursors c ON c.source_id=s.id`

const sourcePollAttemptSelect = `SELECT id,source_id,state,COALESCE(cursor_before,''),COALESCE(cursor_after,''),events_observed,events_inserted,outcomes_observed,outcomes_inserted,routed_runs,event_ids_json,run_ids_json,COALESCE(error_code,''),COALESCE(error_message,''),started_at,finished_at,next_poll_at,rate_limit_reset_at FROM source_poll_attempts`

func scanSource(row rowScanner) (SourceStatus, error) {
	var value SourceStatus
	var lastPoll, lastSuccess, nextPoll, rateReset sql.NullInt64
	var created, updated int64
	err := row.Scan(&value.ID, &value.Kind, &value.ConfigRevision, &value.State, &value.DefinitionEnabled, &value.Paused, &value.LastError, &value.Cursor, &lastPoll, &lastSuccess, &nextPoll, &rateReset, &value.FailureCount, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceStatus{}, ErrNotFound
	}
	if err != nil {
		return SourceStatus{}, err
	}
	value.LastPollAt, value.LastSuccessAt, value.NextPollAt, value.RateLimitResetAt = optionalFromMillis(lastPoll), optionalFromMillis(lastSuccess), optionalFromMillis(nextPoll), optionalFromMillis(rateReset)
	value.CreatedAt, value.UpdatedAt = fromMillis(created), fromMillis(updated)
	return value, nil
}

func scanSourcePollAttempt(row rowScanner) (SourcePollAttempt, error) {
	var value SourcePollAttempt
	var eventIDs, runIDs string
	var finished, nextPoll, rateReset sql.NullInt64
	var started int64
	err := row.Scan(&value.ID, &value.SourceID, &value.State, &value.CursorBefore, &value.CursorAfter,
		&value.EventsObserved, &value.EventsInserted, &value.OutcomesObserved, &value.OutcomesInserted,
		&value.RoutedRuns, &eventIDs, &runIDs, &value.ErrorCode, &value.ErrorMessage, &started,
		&finished, &nextPoll, &rateReset)
	if errors.Is(err, sql.ErrNoRows) {
		return SourcePollAttempt{}, ErrNotFound
	}
	if err != nil {
		return SourcePollAttempt{}, err
	}
	if err := json.Unmarshal([]byte(eventIDs), &value.EventRecordIDs); err != nil {
		return SourcePollAttempt{}, fmt.Errorf("decode source poll event ids: %w", err)
	}
	if err := json.Unmarshal([]byte(runIDs), &value.RunIDs); err != nil {
		return SourcePollAttempt{}, fmt.Errorf("decode source poll run ids: %w", err)
	}
	value.EventRecordIDs, value.RunIDs = normalizedIDs(value.EventRecordIDs), normalizedIDs(value.RunIDs)
	value.StartedAt = fromMillis(started)
	value.FinishedAt, value.NextPollAt, value.RateLimitResetAt = optionalFromMillis(finished), optionalFromMillis(nextPoll), optionalFromMillis(rateReset)
	return value, nil
}

func normalizedIDs(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func optionalMillis(value time.Time) any {
	if value.IsZero() {
		return int64(0)
	}
	return millis(value)
}

func optionalFromMillis(value sql.NullInt64) time.Time {
	if !value.Valid || value.Int64 == 0 {
		return time.Time{}
	}
	return fromMillis(value.Int64)
}
