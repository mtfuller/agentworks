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

// IngestEvent stores an event once per (source, external ID). Duplicate delivery
// returns the original record and created=false.
func (s *Store) IngestEvent(ctx context.Context, event Event) (stored Event, created bool, err error) {
	if err := normalizeEvent(&event); err != nil {
		return Event{}, false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO events (
			record_id, source, external_id, type, subject, occurred_at, received_at,
			data_json, raw_data_json, status, routing_reason, routed_run_id, work_item_id, replay_of
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))
		ON CONFLICT(source, external_id) DO NOTHING
	`, event.RecordID, event.Source, event.ExternalID, event.Type, event.Subject,
		millis(event.OccurredAt), millis(event.ReceivedAt), string(event.Data), string(event.RawData), event.Status,
		event.RoutingReason, event.RoutedRunID, event.WorkItemID, event.ReplayOf)
	if err != nil {
		return Event{}, false, fmt.Errorf("ingest event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Event{}, false, fmt.Errorf("inspect event insert: %w", err)
	}
	stored, err = scanEvent(tx.QueryRowContext(ctx, `SELECT record_id,source,external_id,type,COALESCE(subject,''),occurred_at,received_at,data_json,raw_data_json,status,COALESCE(routing_reason,''),COALESCE(routed_run_id,''),COALESCE(work_item_id,''),COALESCE(replay_of,'') FROM events WHERE source=? AND external_id=?`, event.Source, event.ExternalID))
	if err != nil {
		return Event{}, false, err
	}
	if rows == 1 {
		data, _ := json.Marshal(map[string]any{"status": stored.Status})
		if _, err = appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "event.ingested", EntityType: "event", EntityID: stored.RecordID, Data: data, CreatedAt: stored.ReceivedAt}); err != nil {
			return Event{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Event{}, false, err
	}
	return stored, rows == 1, nil
}

// MarkEventUnrouted records a final no-match or tied-route decision.
func (s *Store) MarkEventUnrouted(ctx context.Context, recordID, reason string, at time.Time) error {
	if strings.TrimSpace(recordID) == "" || strings.TrimSpace(reason) == "" {
		return errors.New("event record ID and routing reason are required")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE events
		SET status = ?, routing_reason = ?, routing_decided_at = ?, routed_run_id = NULL
		WHERE record_id = ? AND status != ?
	`, EventUnrouted, reason, millis(at), recordID, EventRouted)
	if err != nil {
		return fmt.Errorf("mark event unrouted: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect unrouted update: %w", err)
	}
	if rows == 0 {
		var status EventStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM events WHERE record_id = ?`, recordID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("inspect event state: %w", err)
		}
		return fmt.Errorf("%w: event is already %s", ErrConflict, status)
	}
	data, _ := json.Marshal(map[string]any{"status": EventUnrouted, "reason": reason})
	if _, err = appendRuntimeEvent(ctx, tx, RuntimeEvent{Topic: "event.routing", EntityType: "event", EntityID: recordID, Data: data, CreatedAt: at}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) eventBySourceID(ctx context.Context, source, externalID string) (Event, error) {
	return scanEvent(s.db.QueryRowContext(ctx, `
		SELECT record_id, source, external_id, type, COALESCE(subject, ''), occurred_at,
			received_at, data_json, raw_data_json, status, COALESCE(routing_reason, ''),
			COALESCE(routed_run_id, ''), COALESCE(work_item_id, ''), COALESCE(replay_of,'')
		FROM events WHERE source = ? AND external_id = ?
	`, source, externalID))
}

// GetEvent loads a normalized event by its durable record ID.
func (s *Store) GetEvent(ctx context.Context, recordID string) (Event, error) {
	return scanEvent(s.db.QueryRowContext(ctx, `
		SELECT record_id, source, external_id, type, COALESCE(subject, ''), occurred_at,
			received_at, data_json, raw_data_json, status, COALESCE(routing_reason, ''),
			COALESCE(routed_run_id, ''), COALESCE(work_item_id, ''), COALESCE(replay_of,'')
		FROM events WHERE record_id = ?
	`, recordID))
}

func (s *Store) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
	SELECT record_id,source,external_id,type,COALESCE(subject,''),occurred_at,received_at,data_json,raw_data_json,status,COALESCE(routing_reason,''),COALESCE(routed_run_id,''),COALESCE(work_item_id,''),COALESCE(replay_of,'') FROM events ORDER BY received_at DESC,record_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	values := []Event{}
	for rows.Next() {
		value, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListEventsByStatus(ctx context.Context, status EventStatus, limit int) ([]Event, error) {
	if status != EventIngested && status != EventUnrouted && status != EventRouted {
		return nil, errors.New("invalid event status")
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT record_id,source,external_id,type,COALESCE(subject,''),occurred_at,received_at,data_json,raw_data_json,status,COALESCE(routing_reason,''),COALESCE(routed_run_id,''),COALESCE(work_item_id,''),COALESCE(replay_of,'') FROM events WHERE status=? ORDER BY received_at,record_id LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Event{}
	for rows.Next() {
		value, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// ReopenEvent clears a final unrouted decision so the same evaluator can be
// run again after route definitions change. Routed events are immutable.
func (s *Store) ReopenEvent(ctx context.Context, recordID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE events SET status='ingested',routing_reason=NULL,routing_decided_at=NULL WHERE record_id=? AND status='unrouted'`, recordID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		return nil
	}
	var status EventStatus
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM events WHERE record_id=?`, recordID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return fmt.Errorf("%w: event is %s", ErrConflict, status)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var occurredAt, receivedAt int64
	var data, raw string
	if err := row.Scan(&event.RecordID, &event.Source, &event.ExternalID, &event.Type,
		&event.Subject, &occurredAt, &receivedAt, &data, &raw, &event.Status,
		&event.RoutingReason, &event.RoutedRunID, &event.WorkItemID, &event.ReplayOf); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Event{}, ErrNotFound
		}
		return Event{}, fmt.Errorf("scan event: %w", err)
	}
	event.OccurredAt = fromMillis(occurredAt)
	event.ReceivedAt = fromMillis(receivedAt)
	event.Data = json.RawMessage(data)
	event.RawData = json.RawMessage(raw)
	return event, nil
}

func normalizeEvent(event *Event) error {
	event.RecordID = strings.TrimSpace(event.RecordID)
	event.Source = strings.TrimSpace(event.Source)
	event.ExternalID = strings.TrimSpace(event.ExternalID)
	event.Type = strings.TrimSpace(event.Type)
	if event.RecordID == "" || event.Source == "" || event.ExternalID == "" || event.Type == "" {
		return errors.New("event record ID, source, external ID, and type are required")
	}
	if event.OccurredAt.IsZero() {
		return errors.New("event occurrence time is required")
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	if len(event.Data) == 0 {
		event.Data = json.RawMessage(`{}`)
	}
	if len(event.RawData) == 0 {
		event.RawData = append(json.RawMessage(nil), event.Data...)
	}
	if !json.Valid(event.RawData) {
		return errors.New("event raw data must be valid JSON")
	}
	if !json.Valid(event.Data) {
		return errors.New("event data must be valid JSON")
	}
	if event.Status == "" {
		event.Status = EventIngested
	}
	if event.Status != EventIngested && event.Status != EventUnrouted {
		return fmt.Errorf("new event status must be %q or %q", EventIngested, EventUnrouted)
	}
	return nil
}

func millis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}
