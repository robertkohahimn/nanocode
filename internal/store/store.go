package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Store handles persistence of sessions and messages.
type Store interface {
	CreateSession(ctx context.Context, project string) (string, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, project string, limit int) ([]Session, error)
	AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error
	GetMessages(ctx context.Context, sessionID string) ([]MessageRecord, error)
	UpdateSessionTitle(ctx context.Context, id, title string) error
	Close() error
}

type Session struct {
	ID        string
	Project   string
	Title     string
	CreatedAt int64
	UpdatedAt int64
}

type MessageRecord struct {
	ID        string
	SessionID string
	Role      string
	Content   string // JSON-encoded []ContentBlock
	Metadata  string // JSON: {model, usage, duration_ms}
	CreatedAt int64
}

type SQLiteStore struct {
	db *sql.DB
}

// Open opens or creates the SQLite database at the given path.
func Open(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite is single-writer. Limit to one connection to prevent SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	// Set pragmas for performance and correctness
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting pragma: %w", err)
		}
	}

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// OpenMemory opens an in-memory SQLite database (for tests).
func OpenMemory() (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	// Pin to one connection so all queries share the same in-memory database.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, project string) (string, error) {
	id := uuid.New().String()
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, project, title, created_at, updated_at) VALUES (?, ?, '', ?, ?)",
		id, project, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		"SELECT id, project, title, created_at, updated_at FROM sessions WHERE id = ?", id,
	).Scan(&sess.ID, &sess.Project, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}
	return &sess, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, project string, limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, project, title, created_at, updated_at FROM sessions WHERE project = ? ORDER BY updated_at DESC LIMIT ?",
		project, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Project, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error {
	if msg == nil {
		return fmt.Errorf("cannot append nil message")
	}
	id := msg.ID
	if id == "" {
		id = uuid.New().String()
	}
	createdAt := msg.CreatedAt
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	metadata := msg.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"INSERT INTO messages (id, session_id, role, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, sessionID, msg.Role, msg.Content, metadata, createdAt,
	)
	if err != nil {
		return fmt.Errorf("appending message: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE sessions SET updated_at = ? WHERE id = ?",
		createdAt, sessionID,
	)
	if err != nil {
		return fmt.Errorf("updating session timestamp: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, session_id, role, content, metadata, created_at FROM messages WHERE session_id = ? ORDER BY created_at",
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}
	defer rows.Close()

	var messages []MessageRecord
	for rows.Next() {
		var msg MessageRecord
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &msg.Metadata, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *SQLiteStore) UpdateSessionTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET title = ? WHERE id = ?", title, id)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
