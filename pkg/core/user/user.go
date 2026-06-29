// Package user provides SQLite-backed user CRUD operations for the portsari bot.
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// User represents a bot user as stored in the users table.
type User struct {
	ID          int64
	TelegramID  int64
	Username    string
	DisplayName string
	Role        string
	Tier        string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	IsActive    bool
	LastSeen    *time.Time
}

// Store provides CRUD operations for users backed by a shared SQLite database.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a Store backed by the given *sql.DB. The caller must have
// already opened the database with the "sqlite" driver.
func New(db *sql.DB) *Store {
	return &Store{
		db:  db,
		log: slog.With("subsystem", "user"),
	}
}

const tableName = "users"

// ErrNotFound is returned when a user is not found.
var ErrNotFound = errors.New("user: not found")

// Get retrieves a user by telegram_id. Returns ErrNotFound if not found.
func (s *Store) Get(ctx context.Context, telegramID int64) (*User, error) {
	query := fmt.Sprintf(`SELECT id, telegram_id, username, display_name, role, tier, created_at, expires_at, is_active, last_seen FROM %s WHERE telegram_id = ?`, tableName)

	row := s.db.QueryRowContext(ctx, query, telegramID)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user: get: %w", err)
	}
	return u, nil
}

// Create inserts a new user. Returns the created User with its ID filled in.
func (s *Store) Create(ctx context.Context, telegramID int64, username, displayName, role, tier string, expiresAt *time.Time) (*User, error) {
	query := fmt.Sprintf(`INSERT INTO %s (telegram_id, username, display_name, role, tier, expires_at) VALUES (?, ?, ?, ?, ?, ?)`, tableName)

	var expiresAtArg any
	if expiresAt != nil {
		expiresAtArg = expiresAt.UTC().Format(time.DateTime)
	}

	result, err := s.db.ExecContext(ctx, query, telegramID, nullString(username), displayName, role, tier, expiresAtArg)
	if err != nil {
		return nil, fmt.Errorf("user: create: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("user: create: last insert id: %w", err)
	}

	now := time.Now().UTC()
	s.log.Debug("user created", "telegram_id", telegramID, "id", id, "role", role, "tier", tier)

	return &User{
		ID:          id,
		TelegramID:  telegramID,
		Username:    username,
		DisplayName: displayName,
		Role:        role,
		Tier:        tier,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		IsActive:    true,
	}, nil
}

// SetRole updates a user's role.
func (s *Store) SetRole(ctx context.Context, telegramID int64, role string) error {
	query := fmt.Sprintf(`UPDATE %s SET role = ? WHERE telegram_id = ?`, tableName)
	result, err := s.db.ExecContext(ctx, query, role, telegramID)
	if err != nil {
		return fmt.Errorf("user: set role: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: set role: rows affected: %w", err)
	}
	if n == 0 {
		return errors.New("user: set role: user not found")
	}
	s.log.Debug("role updated", "telegram_id", telegramID, "role", role)
	return nil
}

// SetTier updates a user's tier.
func (s *Store) SetTier(ctx context.Context, telegramID int64, tier string) error {
	query := fmt.Sprintf(`UPDATE %s SET tier = ? WHERE telegram_id = ?`, tableName)
	result, err := s.db.ExecContext(ctx, query, tier, telegramID)
	if err != nil {
		return fmt.Errorf("user: set tier: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: set tier: rows affected: %w", err)
	}
	if n == 0 {
		return errors.New("user: set tier: user not found")
	}
	s.log.Debug("tier updated", "telegram_id", telegramID, "tier", tier)
	return nil
}

// SetExpiry sets or clears a user's expires_at.
func (s *Store) SetExpiry(ctx context.Context, telegramID int64, expiresAt *time.Time) error {
	query := fmt.Sprintf(`UPDATE %s SET expires_at = ? WHERE telegram_id = ?`, tableName)

	var expiresAtArg any
	if expiresAt != nil {
		expiresAtArg = expiresAt.UTC().Format(time.DateTime)
	}

	result, err := s.db.ExecContext(ctx, query, expiresAtArg, telegramID)
	if err != nil {
		return fmt.Errorf("user: set expiry: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: set expiry: rows affected: %w", err)
	}
	if n == 0 {
		return errors.New("user: set expiry: user not found")
	}
	s.log.Debug("expiry updated", "telegram_id", telegramID, "expires_at", expiresAt)
	return nil
}

// SetLastSeen updates the last_seen timestamp for a user.
func (s *Store) SetLastSeen(ctx context.Context, telegramID int64, lastSeen time.Time) error {
	query := fmt.Sprintf(`UPDATE %s SET last_seen = ? WHERE telegram_id = ?`, tableName)
	result, err := s.db.ExecContext(ctx, query, lastSeen.UTC().Format(time.DateTime), telegramID)
	if err != nil {
		return fmt.Errorf("user: set last seen: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: set last seen: rows affected: %w", err)
	}
	if n == 0 {
		return errors.New("user: set last seen: user not found")
	}
	s.log.Debug("last_seen updated", "telegram_id", telegramID, "last_seen", lastSeen)
	return nil
}

// Deactivate sets is_active = 0 for a user.
func (s *Store) Deactivate(ctx context.Context, telegramID int64) error {
	query := fmt.Sprintf(`UPDATE %s SET is_active = 0 WHERE telegram_id = ?`, tableName)
	result, err := s.db.ExecContext(ctx, query, telegramID)
	if err != nil {
		return fmt.Errorf("user: deactivate: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("user: deactivate: rows affected: %w", err)
	}
	if n == 0 {
		return errors.New("user: deactivate: user not found")
	}
	s.log.Debug("user deactivated", "telegram_id", telegramID)
	return nil
}

// List returns all users.
func (s *Store) List(ctx context.Context) ([]User, error) {
	query := fmt.Sprintf(`SELECT id, telegram_id, username, display_name, role, tier, created_at, expires_at, is_active, last_seen FROM %s ORDER BY id`, tableName)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("user: list: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("user: list: scan: %w", err)
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list: rows: %w", err)
	}

	if users == nil {
		users = []User{}
	}

	return users, nil
}

// CountAdmins returns the number of admin users.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE role = 'admin' AND is_active = 1`, tableName)

	var count int
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("user: count admins: %w", err)
	}
	return count, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row scannable) (*User, error) {
	var (
		user      User
		username  sql.NullString
		expiresAt sql.NullTime
		lastSeen  sql.NullTime
		isActive  int
	)

	if err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&username,
		&user.DisplayName,
		&user.Role,
		&user.Tier,
		&user.CreatedAt,
		&expiresAt,
		&isActive,
		&lastSeen,
	); err != nil {
		return nil, err
	}

	user.Username = username.String
	user.IsActive = isActive == 1

	if expiresAt.Valid {
		user.ExpiresAt = &expiresAt.Time
	}
	if lastSeen.Valid {
		user.LastSeen = &lastSeen.Time
	}

	return &user, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
