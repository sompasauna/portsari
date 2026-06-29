package accessrequest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE access_requests (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id  INTEGER NOT NULL,
			username     TEXT,
			display_name TEXT NOT NULL,
			requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected'))
		)`)
	require.NoError(t, err)
	return db
}

func TestCreate(t *testing.T) {
	s := New(newTestDB(t))
	ctx := context.Background()

	req, err := s.Create(ctx, 1001, "kimmo", "Kimmo Lehto")
	require.NoError(t, err)
	assert.Equal(t, int64(1001), req.TelegramID)
	assert.Equal(t, "kimmo", req.Username)
	assert.Equal(t, "pending", req.Status)
}

func TestGetLatestByTelegramID_NotFound(t *testing.T) {
	s := New(newTestDB(t))
	_, err := s.GetLatestByTelegramID(context.Background(), 9999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetLatestByTelegramID_ReturnsNewest(t *testing.T) {
	s := New(newTestDB(t))
	ctx := context.Background()

	_, err := s.Create(ctx, 1001, "kimmo", "Kimmo")
	require.NoError(t, err)
	// Reject it so a second request can be created.
	require.NoError(t, s.Reject(ctx, 1001))
	second, err := s.Create(ctx, 1001, "kimmo", "Kimmo")
	require.NoError(t, err)

	got, err := s.GetLatestByTelegramID(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, second.ID, got.ID)
	assert.Equal(t, "pending", got.Status)
}

func TestListPending(t *testing.T) {
	s := New(newTestDB(t))
	ctx := context.Background()

	_, err := s.Create(ctx, 1001, "a", "Alice")
	require.NoError(t, err)
	_, err = s.Create(ctx, 1002, "b", "Bob")
	require.NoError(t, err)
	require.NoError(t, s.Reject(ctx, 1002))

	pending, err := s.ListPending(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, int64(1001), pending[0].TelegramID)
}

func TestApprove(t *testing.T) {
	s := New(newTestDB(t))
	ctx := context.Background()

	_, err := s.Create(ctx, 1001, "", "Alice")
	require.NoError(t, err)
	require.NoError(t, s.Approve(ctx, 1001))

	req, err := s.GetLatestByTelegramID(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "approved", req.Status)
}

func TestApprove_NoPending(t *testing.T) {
	s := New(newTestDB(t))
	err := s.Approve(context.Background(), 9999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestReject(t *testing.T) {
	s := New(newTestDB(t))
	ctx := context.Background()

	_, err := s.Create(ctx, 1001, "", "Alice")
	require.NoError(t, err)
	require.NoError(t, s.Reject(ctx, 1001))

	req, err := s.GetLatestByTelegramID(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "rejected", req.Status)
}

func TestDedupe_PendingRequestAlreadyExists(t *testing.T) {
	s := New(newTestDB(t))
	ctx := context.Background()

	_, err := s.Create(ctx, 1001, "", "Alice")
	require.NoError(t, err)

	// Simulating the bot-level dedupe check.
	existing, err := s.GetLatestByTelegramID(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "pending", existing.Status)
}
