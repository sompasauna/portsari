// Package accesslog provides SQLite-backed persistence for door access events.
package accesslog

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Entry represents a single access log row.
type Entry struct {
	ID              int64
	OccurredAt      time.Time
	ActorTelegramID *int64 // nil when not bot-mediated (keypad/auto-relock)
	Action          string // "lock", "unlock", "denied_tier", "denied_expired", "denied_rate_limit", "state_change"
	Result          string // "success", "denied", "error", "timeout"
	Detail          string
}

// Store wraps the access_log table.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a Store backed by the given *sql.DB.
func New(db *sql.DB) *Store {
	return &Store{
		db:  db,
		log: slog.With("subsystem", "accesslog"),
	}
}

// Write inserts an access log entry.
func (s *Store) Write(ctx context.Context, e Entry) error {
	e.OccurredAt = e.OccurredAt.UTC()

	var actorID any
	if e.ActorTelegramID != nil {
		actorID = *e.ActorTelegramID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO access_log (occurred_at, actor_telegram_id, action, result, detail) VALUES (?, ?, ?, ?, ?)`,
		e.OccurredAt, actorID, e.Action, e.Result, e.Detail)
	if err != nil {
		return fmt.Errorf("accesslog: write: %w", err)
	}

	return nil
}

// List returns the n most recent entries, ordered by id DESC.
func (s *Store) List(ctx context.Context, n int) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, occurred_at, actor_telegram_id, action, result, COALESCE(detail, '') FROM access_log ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("accesslog: list: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var actorID sql.NullInt64

		if err := rows.Scan(&e.ID, &e.OccurredAt, &actorID, &e.Action, &e.Result, &e.Detail); err != nil {
			return nil, fmt.Errorf("accesslog: list: scan: %w", err)
		}
		if actorID.Valid {
			e.ActorTelegramID = &actorID.Int64
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("accesslog: list: rows: %w", err)
	}

	return entries, nil
}

// RecentSuccessCount returns the number of "unlock" / "success" entries
// for a given telegram_id within the last windowMinutes minutes.
func (s *Store) RecentSuccessCount(ctx context.Context, telegramID int64, windowMinutes int) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM access_log WHERE actor_telegram_id = ? AND action = 'unlock' AND result = 'success' AND occurred_at >= datetime('now', '-' || ? || ' minutes')`,
		telegramID, windowMinutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("accesslog: recent success count: %w", err)
	}

	return count, nil
}
