package bot

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/core/user"
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

func makeMessageUpdate(fromID int64, username, text string) telego.Update {
	return telego.Update{
		Message: &telego.Message{
			From: &telego.User{ID: fromID, Username: username, FirstName: username},
			Text: text,
		},
	}
}

func TestResolveUserCreatesBootstrapAdmin(t *testing.T) {
	db := newTestDB(t)
	store := user.New(db)
	ctx := context.Background()

	update := makeMessageUpdate(1001, "admin_user", "/start")
	bootstrapID := int64(1001)

	u, err := ResolveUser(ctx, store, update, bootstrapID)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "admin", u.Role)
	assert.Equal(t, int64(1001), u.TelegramID)

	count, err := store.CountAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestResolveUserUnknownReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	store := user.New(db)
	ctx := context.Background()

	update := makeMessageUpdate(2001, "normal_user", "/start")
	bootstrapID := int64(1001)

	u, err := ResolveUser(ctx, store, update, bootstrapID)
	require.ErrorIs(t, err, user.ErrNotFound)
	assert.Nil(t, u)

	// No user row created.
	_, getErr := store.Get(ctx, 2001)
	assert.ErrorIs(t, getErr, user.ErrNotFound)
}

func TestResolveUserBootstrapSkippedWhenAdminExists(t *testing.T) {
	db := newTestDB(t)
	store := user.New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 9001, "existing_admin", "Existing Admin", "admin", "full", nil)
	require.NoError(t, err)

	update := makeMessageUpdate(1001, "new_guy", "/start")
	bootstrapID := int64(1001)

	// Bootstrap user must go through the request flow once an admin exists.
	u, err := ResolveUser(ctx, store, update, bootstrapID)
	require.ErrorIs(t, err, user.ErrNotFound)
	assert.Nil(t, u)
}

func TestResolveUserRejectsInactive(t *testing.T) {
	db := newTestDB(t)
	store := user.New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 3001, "inactive_user", "Inactive User", "user", "full", nil)
	require.NoError(t, err)

	err = store.Deactivate(ctx, 3001)
	require.NoError(t, err)

	update := makeMessageUpdate(3001, "inactive_user", "/start")

	u, err := ResolveUser(ctx, store, update, 0)
	require.Error(t, err)
	assert.Nil(t, u)
	assert.Contains(t, err.Error(), "inactive")
}

func TestResolveUserRejectsExpired(t *testing.T) {
	db := newTestDB(t)
	store := user.New(db)
	ctx := context.Background()

	expiredAt := time.Now().Add(-time.Minute)
	_, err := store.Create(ctx, 3002, "expired_user", "Expired User", "user", "full", &expiredAt)
	require.NoError(t, err)

	update := makeMessageUpdate(3002, "expired_user", "/start")

	u, err := ResolveUser(ctx, store, update, 0)
	require.Error(t, err)
	assert.Nil(t, u)
	assert.Contains(t, err.Error(), "expired")
}

func TestResolveUserUpdatesLastSeen(t *testing.T) {
	db := newTestDB(t)
	store := user.New(db)
	ctx := context.Background()

	_, err := store.Create(ctx, 4001, "seen_user", "Seen User", "user", "full", nil)
	require.NoError(t, err)

	u, err := store.Get(ctx, 4001)
	require.NoError(t, err)
	assert.Nil(t, u.LastSeen)

	update := makeMessageUpdate(4001, "seen_user", "/start")
	_, err = ResolveUser(ctx, store, update, 0)
	require.NoError(t, err)

	u, err = store.Get(ctx, 4001)
	require.NoError(t, err)
	require.NotNil(t, u.LastSeen)
	assert.False(t, u.LastSeen.IsZero())
}

func TestRequireAdmin(t *testing.T) {
	admin := &user.User{Role: "admin"}
	normal := &user.User{Role: "user"}

	assert.NoError(t, RequireAdmin(admin))
	assert.Error(t, RequireAdmin(normal))
}

func TestResolveUserNoUserInUpdate(t *testing.T) {
	store := user.New(nil)
	ctx := context.Background()

	update := telego.Update{}

	u, err := ResolveUser(ctx, store, update, 0)
	require.Error(t, err)
	assert.Nil(t, u)
	assert.Contains(t, err.Error(), "no user in update")
}
