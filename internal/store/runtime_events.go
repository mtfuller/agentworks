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

// RuntimeEvent is a small durable notification pointing at authoritative state.
// Raw logs and request payloads do not belong in this journal.
type RuntimeEvent struct {
	Sequence   int64           `json:"sequence"`
	Topic      string          `json:"topic"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Data       json.RawMessage `json:"data"`
	CreatedAt  time.Time       `json:"created_at"`
}

type runtimeEventExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// AppendRuntimeEvent records a replayable notification. Callers should include
// identifiers and state only; consumers reload authoritative records by ID.
func (s *Store) AppendRuntimeEvent(ctx context.Context, event RuntimeEvent) (RuntimeEvent, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return appendRuntimeEvent(ctx, s.db, event)
}

func appendRuntimeEvent(ctx context.Context, execer runtimeEventExecer, event RuntimeEvent) (RuntimeEvent, error) {
	event.Topic = strings.TrimSpace(event.Topic)
	event.EntityType = strings.TrimSpace(event.EntityType)
	event.EntityID = strings.TrimSpace(event.EntityID)
	if event.Topic == "" || event.EntityType == "" || event.EntityID == "" {
		return RuntimeEvent{}, errors.New("runtime event topic, entity type, and entity ID are required")
	}
	if len(event.Data) == 0 {
		event.Data = json.RawMessage(`{}`)
	}
	if !json.Valid(event.Data) {
		return RuntimeEvent{}, errors.New("runtime event data must be valid JSON")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	result, err := execer.ExecContext(ctx, `
		INSERT INTO runtime_events(topic, entity_type, entity_id, data_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, event.Topic, event.EntityType, event.EntityID, string(event.Data), millis(event.CreatedAt))
	if err != nil {
		return RuntimeEvent{}, fmt.Errorf("append runtime event: %w", err)
	}
	event.Sequence, err = result.LastInsertId()
	if err != nil {
		return RuntimeEvent{}, fmt.Errorf("read runtime event sequence: %w", err)
	}
	return event, nil
}

func appendRunStateEvent(ctx context.Context, execer runtimeEventExecer, runID string, state RunState, attemptID string, at time.Time) error {
	data, _ := json.Marshal(map[string]any{"state": state, "attempt_id": attemptID})
	_, err := appendRuntimeEvent(ctx, execer, RuntimeEvent{
		Topic: "run.state", EntityType: "run", EntityID: runID, Data: data, CreatedAt: at,
	})
	return err
}

// ListRuntimeEventsAfter returns events in strict sequence order. The limit is
// clamped so one reconnect cannot monopolize a Studio request.
func (s *Store) ListRuntimeEventsAfter(ctx context.Context, sequence int64, limit int) ([]RuntimeEvent, error) {
	if sequence < 0 {
		return nil, errors.New("runtime event sequence cannot be negative")
	}
	if limit <= 0 {
		limit = 100
	} else if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, topic, entity_type, entity_id, data_json, created_at
		FROM runtime_events WHERE sequence > ? ORDER BY sequence LIMIT ?
	`, sequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list runtime events: %w", err)
	}
	defer rows.Close()
	events := make([]RuntimeEvent, 0)
	for rows.Next() {
		var event RuntimeEvent
		var data string
		var createdAt int64
		if err := rows.Scan(&event.Sequence, &event.Topic, &event.EntityType, &event.EntityID, &data, &createdAt); err != nil {
			return nil, fmt.Errorf("scan runtime event: %w", err)
		}
		event.Data = json.RawMessage(data)
		event.CreatedAt = fromMillis(createdAt)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runtime events: %w", err)
	}
	return events, nil
}
