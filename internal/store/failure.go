package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrFailureNotFound = errors.New("failure not found")

// FailureRecord captures a single failure event.
type FailureRecord struct {
	ID           string
	SessionID    string
	Timestamp    int64
	FailureType  string
	Description  string
	ToolsUsed    string // JSON array
	FilesTouched string // JSON array
	Iterations   int
	Notes        string
}

// MarshalStringSlice converts a string slice to a JSON array string.
func MarshalStringSlice(ss []string) string {
	if ss == nil {
		ss = []string{}
	}
	b, _ := json.Marshal(ss)
	return string(b)
}

func (s *SQLiteStore) CreateFailure(ctx context.Context, rec *FailureRecord) error {
	if rec == nil {
		return fmt.Errorf("cannot create nil failure record")
	}
	if rec.ID == "" {
		rec.ID = uuid.New().String()
	}
	if rec.Timestamp == 0 {
		rec.Timestamp = time.Now().Unix()
	}
	if rec.ToolsUsed == "" {
		rec.ToolsUsed = "[]"
	}
	if rec.FilesTouched == "" {
		rec.FilesTouched = "[]"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO failures (id, session_id, timestamp, failure_type, description, tools_used, files_touched, iterations, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.SessionID, rec.Timestamp, rec.FailureType,
		rec.Description, rec.ToolsUsed, rec.FilesTouched, rec.Iterations, rec.Notes,
	)
	if err != nil {
		return fmt.Errorf("creating failure: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListFailures(ctx context.Context, since int64, limit int) ([]FailureRecord, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}
	const maxLimit = 1000
	if limit > maxLimit {
		limit = maxLimit
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, timestamp, failure_type, description, tools_used, files_touched, iterations, notes
		 FROM failures WHERE timestamp >= ? ORDER BY timestamp DESC LIMIT ?`,
		since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing failures: %w", err)
	}
	defer rows.Close()

	var records []FailureRecord
	for rows.Next() {
		var r FailureRecord
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Timestamp, &r.FailureType,
			&r.Description, &r.ToolsUsed, &r.FilesTouched, &r.Iterations, &r.Notes); err != nil {
			return nil, fmt.Errorf("scanning failure: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating failures: %w", err)
	}
	return records, nil
}

func (s *SQLiteStore) GetFailure(ctx context.Context, id string) (*FailureRecord, error) {
	var r FailureRecord
	err := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, timestamp, failure_type, description, tools_used, files_touched, iterations, notes
		 FROM failures WHERE id = ?`, id,
	).Scan(&r.ID, &r.SessionID, &r.Timestamp, &r.FailureType,
		&r.Description, &r.ToolsUsed, &r.FilesTouched, &r.Iterations, &r.Notes)
	if err != nil {
		return nil, fmt.Errorf("getting failure %s: %w", id, err)
	}
	return &r, nil
}

func (s *SQLiteStore) AnnotateFailure(ctx context.Context, id string, failureType string, notes string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE failures SET failure_type = ?, notes = ? WHERE id = ?`,
		failureType, notes, id,
	)
	if err != nil {
		return fmt.Errorf("annotating failure: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("failure %s: %w", id, ErrFailureNotFound)
	}
	return nil
}
