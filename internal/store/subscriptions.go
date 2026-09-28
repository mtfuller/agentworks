package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (s *Store) SyncSubscription(ctx context.Context, value Subscription) error {
	if value.LoadedAt.IsZero() {
		value.LoadedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO subscriptions(id,revision,enabled,priority,loaded_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,priority=excluded.priority,loaded_at=excluded.loaded_at`, value.ID, value.Revision, value.Enabled, value.Priority, millis(value.LoadedAt))
	return err
}
func (s *Store) SetSubscriptionEnabled(ctx context.Context, id string, enabled bool, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET enabled=?,loaded_at=? WHERE id=?`, enabled, millis(at), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,enabled,priority,loaded_at FROM subscriptions ORDER BY priority DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Subscription{}
	for rows.Next() {
		var v Subscription
		var at int64
		if err := rows.Scan(&v.ID, &v.Revision, &v.Enabled, &v.Priority, &at); err != nil {
			return nil, err
		}
		v.LoadedAt = fromMillis(at)
		values = append(values, v)
	}
	return values, rows.Err()
}
func (s *Store) SubscriptionEnabled(ctx context.Context, id string, definitionDefault bool) (bool, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM subscriptions WHERE id=?`, id).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return definitionDefault, nil
	}
	return enabled, err
}

func (s *Store) UpsertScheduleTimer(ctx context.Context, id string, due time.Time, payload json.RawMessage, now time.Time) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return errors.New("timer payload must be valid JSON")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO timers(id,idempotency_key,kind,due_at,state,payload_json,created_at,updated_at) VALUES(?,?,'schedule',?,'pending',?,?,?) ON CONFLICT(id) DO UPDATE SET payload_json=excluded.payload_json,state='pending',updated_at=excluded.updated_at`, id, id, millis(due), string(payload), millis(now), millis(now))
	return err
}
func (s *Store) NextScheduleDue(ctx context.Context) (time.Time, bool, error) {
	var value sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(due_at) FROM timers WHERE kind='schedule' AND state='pending'`).Scan(&value); err != nil {
		return time.Time{}, false, err
	}
	if !value.Valid {
		return time.Time{}, false, nil
	}
	return fromMillis(value.Int64), true, nil
}
func (s *Store) DueScheduleTimers(ctx context.Context, now time.Time, limit int) ([]Timer, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,idempotency_key,kind,due_at,state,payload_json,created_at,updated_at FROM timers WHERE kind='schedule' AND state='pending' AND due_at<=? ORDER BY due_at,id LIMIT ?`, millis(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Timer{}
	for rows.Next() {
		var v Timer
		var due, created, updated int64
		var payload string
		if err := rows.Scan(&v.ID, &v.IdempotencyKey, &v.Kind, &due, &v.State, &payload, &created, &updated); err != nil {
			return nil, err
		}
		v.DueAt = fromMillis(due)
		v.Payload = json.RawMessage(payload)
		v.CreatedAt = fromMillis(created)
		v.UpdatedAt = fromMillis(updated)
		values = append(values, v)
	}
	return values, rows.Err()
}
func (s *Store) AdvanceScheduleTimer(ctx context.Context, id string, expected, next time.Time, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE timers SET due_at=?,updated_at=? WHERE id=? AND kind='schedule' AND state='pending' AND due_at=?`, millis(next), millis(now), id, millis(expected))
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("%w: schedule timer changed", ErrConflict)
	}
	return nil
}

func (s *Store) CancelScheduleTimersExcept(ctx context.Context, active map[string]bool, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM timers WHERE kind='schedule'`)
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
		if active[id] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE timers SET state='cancelled',updated_at=? WHERE id=? AND kind='schedule'`, millis(now), id); err != nil {
			return err
		}
	}
	return nil
}
