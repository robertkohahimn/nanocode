package store

import (
	"database/sql"
	"fmt"
)

var migrations = []string{
	// Version 1: initial schema
	`CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		project    TEXT NOT NULL,
		title      TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
	CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
	CREATE TABLE IF NOT EXISTS messages (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL REFERENCES sessions(id),
		role       TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
		content    TEXT NOT NULL,
		metadata   TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);`,

	// Version 2: snapshots for git tracking
	`CREATE TABLE IF NOT EXISTS snapshots (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		file_path  TEXT NOT NULL,
		git_hash   TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_snapshots_session ON snapshots(session_id, created_at);`,

	// Version 3: add ON DELETE CASCADE to messages table
	// SQLite doesn't support ALTER TABLE for foreign key changes, so we recreate the table
	`CREATE TABLE messages_new (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		role       TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
		content    TEXT NOT NULL,
		metadata   TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL
	);
	INSERT INTO messages_new SELECT * FROM messages;
	DROP TABLE messages;
	ALTER TABLE messages_new RENAME TO messages;
	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);`,

	// Version 4: failure case collection
	`CREATE TABLE IF NOT EXISTS failures (
		id            TEXT PRIMARY KEY,
		session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		timestamp     INTEGER NOT NULL,
		failure_type  TEXT NOT NULL,
		description   TEXT NOT NULL DEFAULT '',
		tools_used    TEXT NOT NULL DEFAULT '[]',
		files_touched TEXT NOT NULL DEFAULT '[]',
		iterations    INTEGER NOT NULL DEFAULT 0,
		notes         TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_failures_session ON failures(session_id);
	CREATE INDEX IF NOT EXISTS idx_failures_timestamp ON failures(timestamp DESC);`,
}

// Migrate ensures the database schema is up to date.
// Uses SQLite's PRAGMA user_version for version tracking.
func Migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this binary supports (max %d); upgrade nanocode", version, len(migrations))
	}

	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration %d: %w", i+1, err)
		}

		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying migration %d: %w", i+1, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", i+1, err)
		}

		// PRAGMA user_version is not transactional in SQLite, so set it after commit
		// This ensures version is only bumped after migration succeeds
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			return fmt.Errorf("setting version %d: %w", i+1, err)
		}
	}

	return nil
}
