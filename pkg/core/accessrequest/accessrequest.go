// Package accessrequest manages pending user access requests backed by SQLite.
package accessrequest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrNotFound is returned when a request is not found.
var ErrNotFound = errors.New("accessrequest: not found")

// Request is a single access request row.
type Request struct {
	ID          int64
	TelegramID  int64
	Username    string
	DisplayName string
	RequestedAt time.Time
	Status      string // pending, approved, rejected
}

// Store provides access request CRUD backed by a shared SQLite database.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a Store backed by the given *sql.DB.
func New(db *sql.DB) *Store {
	return &Store{db: db, log: slog.With("subsystem", "accessrequest")}
}

// Create inserts a new pending access request.
func (s *Store) Create(ctx context.Context, telegramID int64, username, displayName string) (*Request, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO access_requests (telegram_id, username, display_name) VALUES (?, ?, ?)`,
		telegramID, nullString(username), displayName,
	)
	if err != nil {
		return nil, fmt.Errorf("accessrequest: create: %w", err)
	}
	return s.GetLatestByTelegramID(ctx, telegramID)
}

// GetLatestByTelegramID returns the most recent request for the given telegram ID.
func (s *Store) GetLatestByTelegramID(ctx context.Context, telegramID int64) (*Request, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, telegram_id, username, display_name, requested_at, status
		 FROM access_requests WHERE telegram_id = ? ORDER BY id DESC LIMIT 1`,
		telegramID,
	)
	r, err := scanRequest(row)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ListPending returns all requests with status 'pending', oldest first.
func (s *Store) ListPending(ctx context.Context) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, telegram_id, username, display_name, requested_at, status
		 FROM access_requests WHERE status = 'pending' ORDER BY requested_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("accessrequest: list pending: %w", err)
	}
	defer rows.Close()

	var reqs []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("accessrequest: list pending: %w", err)
		}
		reqs = append(reqs, *r)
	}
	if reqs == nil {
		reqs = []Request{}
	}
	return reqs, rows.Err()
}

// Approve marks the latest pending request for the given telegram ID as approved.
func (s *Store) Approve(ctx context.Context, telegramID int64) error {
	return s.setStatus(ctx, telegramID, "approved")
}

// Reject marks the latest pending request for the given telegram ID as rejected.
func (s *Store) Reject(ctx context.Context, telegramID int64) error {
	return s.setStatus(ctx, telegramID, "rejected")
}

func (s *Store) setStatus(ctx context.Context, telegramID int64, status string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE access_requests SET status = ? WHERE telegram_id = ? AND status = 'pending'`,
		status, telegramID,
	)
	if err != nil {
		return fmt.Errorf("accessrequest: set status: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("accessrequest: %w", ErrNotFound)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanRequest(row scannable) (*Request, error) {
	var (
		r        Request
		username sql.NullString
	)
	if err := row.Scan(&r.ID, &r.TelegramID, &username, &r.DisplayName, &r.RequestedAt, &r.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("accessrequest: scan: %w", err)
	}
	r.Username = username.String
	return &r, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
