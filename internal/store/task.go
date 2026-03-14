package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Task represents a tracked work item within a session.
type Task struct {
	ID          string
	SessionID   string
	Subject     string
	Description string
	Status      string // "pending", "in_progress", "completed"
	BlockedBy   []string
	CreatedAt   int64
	UpdatedAt   int64
}

// TaskUpdate holds optional fields for updating a task.
type TaskUpdate struct {
	Subject     *string
	Description *string
	Status      *string
}

func (s *SQLiteStore) CreateTask(ctx context.Context, sessionID, subject, description string) (string, error) {
	id := uuid.New().String()
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO tasks (id, session_id, subject, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'pending', ?, ?)",
		id, sessionID, subject, description, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("creating task: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) UpdateTask(ctx context.Context, id string, updates TaskUpdate) error {
	now := time.Now().Unix()
	query := "UPDATE tasks SET updated_at = ?"
	args := []interface{}{now}

	if updates.Subject != nil {
		query += ", subject = ?"
		args = append(args, *updates.Subject)
	}
	if updates.Description != nil {
		query += ", description = ?"
		args = append(args, *updates.Description)
	}
	if updates.Status != nil {
		query += ", status = ?"
		args = append(args, *updates.Status)
	}

	query += " WHERE id = ?"
	args = append(args, id)

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking update result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	return nil
}

func (s *SQLiteStore) GetTask(ctx context.Context, id string) (*Task, error) {
	var task Task
	err := s.db.QueryRowContext(ctx,
		"SELECT id, session_id, subject, description, status, created_at, updated_at FROM tasks WHERE id = ?", id,
	).Scan(&task.ID, &task.SessionID, &task.Subject, &task.Description, &task.Status, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting task %s: %w", id, err)
	}

	// Load dependencies
	rows, err := s.db.QueryContext(ctx, "SELECT blocked_by FROM task_deps WHERE task_id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("loading task deps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, fmt.Errorf("scanning task dep: %w", err)
		}
		task.BlockedBy = append(task.BlockedBy, dep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task deps: %w", err)
	}

	return &task, nil
}

func (s *SQLiteStore) ListTasks(ctx context.Context, sessionID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, session_id, subject, description, status, created_at, updated_at FROM tasks WHERE session_id = ? ORDER BY created_at",
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Subject, &t.Description, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tasks: %w", err)
	}

	// Load dependencies for each task
	for i := range tasks {
		depRows, err := s.db.QueryContext(ctx, "SELECT blocked_by FROM task_deps WHERE task_id = ?", tasks[i].ID)
		if err != nil {
			return nil, fmt.Errorf("loading deps for task %s: %w", tasks[i].ID, err)
		}
		for depRows.Next() {
			var dep string
			if err := depRows.Scan(&dep); err != nil {
				depRows.Close()
				return nil, fmt.Errorf("scanning dep: %w", err)
			}
			tasks[i].BlockedBy = append(tasks[i].BlockedBy, dep)
		}
		depRows.Close()
		if err := depRows.Err(); err != nil {
			return nil, fmt.Errorf("iterating deps: %w", err)
		}
	}

	return tasks, nil
}

func (s *SQLiteStore) DeleteTask(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking delete result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	return nil
}

func (s *SQLiteStore) AddTaskDep(ctx context.Context, taskID, blockedBy string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO task_deps (task_id, blocked_by) VALUES (?, ?)",
		taskID, blockedBy,
	)
	if err != nil {
		return fmt.Errorf("adding task dependency: %w", err)
	}
	return nil
}
