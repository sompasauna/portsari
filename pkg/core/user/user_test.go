package user

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	schema := `
	CREATE TABLE users (
		id                INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id       INTEGER UNIQUE NOT NULL,
		username          TEXT,
		display_name      TEXT NOT NULL,
		role              TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
		tier              TEXT NOT NULL DEFAULT 'full' CHECK (tier IN ('full', 'daytime')),
		created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at        DATETIME,
		is_active         INTEGER NOT NULL DEFAULT 1,
		last_seen         DATETIME
	)`

	_, err = db.ExecContext(context.Background(), schema)
	require.NoError(t, err)

	return db
}

func TestCreateAndGet(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	u, err := store.Create(ctx, 1001, "alice", "Alice", "admin", "full", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), u.ID)
	assert.Equal(t, int64(1001), u.TelegramID)
	assert.Equal(t, "alice", u.Username)
	assert.Equal(t, "Alice", u.DisplayName)
	assert.Equal(t, "admin", u.Role)
	assert.Equal(t, "full", u.Tier)
	assert.True(t, u.IsActive)
	assert.Nil(t, u.ExpiresAt)
	assert.False(t, u.CreatedAt.IsZero())

	got, err := store.Get(ctx, 1001)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, u.TelegramID, got.TelegramID)
	assert.Equal(t, u.Username, got.Username)
	assert.Equal(t, u.DisplayName, got.DisplayName)
	assert.Equal(t, u.Role, got.Role)
	assert.Equal(t, u.Tier, got.Tier)
	assert.True(t, got.IsActive)
	assert.Nil(t, got.ExpiresAt)
	assert.WithinDuration(t, time.Now(), got.CreatedAt, 5*time.Second)
}

func TestGetNonExistent(t *testing.T) {
	db := newTestDB(t)
	store := New(db)

	got, err := store.Get(context.Background(), 9999)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, got)
}

func TestCreateDuplicateTelegramID(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 1001, "alice", "Alice", "user", "full", nil)
	require.NoError(t, err)

	_, err = store.Create(ctx, 1001, "bob", "Bob", "user", "full", nil)
	require.Error(t, err)
}

func TestSetRoleSetTierDeactivate(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 1001, "alice", "Alice", "user", "full", nil)
	require.NoError(t, err)

	err = store.SetRole(ctx, 1001, "admin")
	require.NoError(t, err)

	got, err := store.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "admin", got.Role)

	err = store.SetTier(ctx, 1001, "daytime")
	require.NoError(t, err)

	got, err = store.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "daytime", got.Tier)

	err = store.Deactivate(ctx, 1001)
	require.NoError(t, err)

	got, err = store.Get(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, got.IsActive)
}

func TestSetRoleNotFound(t *testing.T) {
	db := newTestDB(t)
	store := New(db)

	err := store.SetRole(context.Background(), 9999, "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestSetTierNotFound(t *testing.T) {
	db := newTestDB(t)
	store := New(db)

	err := store.SetTier(context.Background(), 9999, "daytime")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestDeactivateNotFound(t *testing.T) {
	db := newTestDB(t)
	store := New(db)

	err := store.Deactivate(context.Background(), 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestList(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	users, err := store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, users)

	_, err = store.Create(ctx, 1001, "alice", "Alice", "admin", "full", nil)
	require.NoError(t, err)

	_, err = store.Create(ctx, 1002, "bob", "Bob", "user", "daytime", nil)
	require.NoError(t, err)

	users, err = store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 2)
}

func TestCountAdmins(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	n, err := store.CountAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	store.Create(ctx, 1001, "alice", "Alice", "user", "full", nil)
	store.Create(ctx, 1002, "bob", "Bob", "admin", "full", nil)
	store.Create(ctx, 1003, "carol", "Carol", "admin", "full", nil)

	n, err = store.CountAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Deactivated admin should not be counted
	err = store.Deactivate(ctx, 1002)
	require.NoError(t, err)

	n, err = store.CountAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestExpiresAtRoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	expiry := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	t.Run("create with expires_at", func(t *testing.T) {
		_, err := store.Create(ctx, 1001, "alice", "Alice", "user", "full", &expiry)
		require.NoError(t, err)

		got, err := store.Get(ctx, 1001)
		require.NoError(t, err)
		require.NotNil(t, got.ExpiresAt)
		assert.True(t, got.ExpiresAt.Equal(expiry), "expected %v, got %v", expiry, got.ExpiresAt)
	})

	t.Run("set expiry on existing user", func(t *testing.T) {
		err := store.SetExpiry(ctx, 1001, &expiry)
		require.NoError(t, err)

		got, err := store.Get(ctx, 1001)
		require.NoError(t, err)
		require.NotNil(t, got.ExpiresAt)
		assert.True(t, got.ExpiresAt.Equal(expiry))
	})

	t.Run("clear expiry", func(t *testing.T) {
		err := store.SetExpiry(ctx, 1001, nil)
		require.NoError(t, err)

		got, err := store.Get(ctx, 1001)
		require.NoError(t, err)
		assert.Nil(t, got.ExpiresAt)
	})
}

func TestSetExpiryNotFound(t *testing.T) {
	db := newTestDB(t)
	store := New(db)

	expiry := time.Now()
	err := store.SetExpiry(context.Background(), 9999, &expiry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestIsActiveFalse(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 1001, "alice", "Alice", "user", "full", nil)
	require.NoError(t, err)

	got, err := store.Get(ctx, 1001)
	require.NoError(t, err)
	assert.True(t, got.IsActive)

	err = store.Deactivate(ctx, 1001)
	require.NoError(t, err)

	got, err = store.Get(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, got.IsActive)
}

func TestNullUsername(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 1001, "", "NoUsername", "user", "full", nil)
	require.NoError(t, err)

	got, err := store.Get(ctx, 1001)
	require.NoError(t, err)
	assert.Empty(t, got.Username)
}

func TestConsecutiveIDs(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	u1, err := store.Create(ctx, 1001, "a", "A", "user", "full", nil)
	require.NoError(t, err)

	u2, err := store.Create(ctx, 1002, "b", "B", "user", "full", nil)
	require.NoError(t, err)

	assert.Equal(t, int64(1), u2.ID-u1.ID)
}
