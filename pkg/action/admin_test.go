package action

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/sompasauna/portsari/pkg/core/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestUserDB(t *testing.T) *user.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.ExecContext(context.Background(), `
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
		)
	`)
	require.NoError(t, err)

	return user.New(db)
}

type broadcastCall struct {
	chatID int64
	text   string
}

func TestBroadcastExecute_SendMessageToActiveUsers(t *testing.T) {
	userStore := newTestUserDB(t)
	ctx := context.Background()

	_, err := userStore.Create(ctx, 100, "user1", "User One", "user", "full", nil)
	require.NoError(t, err)
	_, err = userStore.Create(ctx, 200, "user2", "User Two", "user", "full", nil)
	require.NoError(t, err)

	var calls []broadcastCall

	a := &BroadcastAction{
		userStore: userStore,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		SendMessage: func(chatID int64, text string) {
			calls = append(calls, broadcastCall{chatID, text})
		},
	}

	err = a.Execute(ctx, "Test broadcast message")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, int64(100), calls[0].chatID)
	assert.Equal(t, "\U0001f4e2 Test broadcast message", calls[0].text)
	assert.Equal(t, int64(200), calls[1].chatID)
}

func TestBroadcastExecute_SkipsInactiveUsers(t *testing.T) {
	userStore := newTestUserDB(t)
	ctx := context.Background()

	_, err := userStore.Create(ctx, 100, "active", "Active", "user", "full", nil)
	require.NoError(t, err)
	_, err = userStore.Create(ctx, 200, "inactive", "Inactive", "user", "full", nil)
	require.NoError(t, err)
	err = userStore.Deactivate(ctx, 200)
	require.NoError(t, err)

	var sentCount int

	a := &BroadcastAction{
		userStore: userStore,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		SendMessage: func(chatID int64, text string) {
			sentCount++
		},
	}

	err = a.Execute(ctx, "Test")
	require.NoError(t, err)
	assert.Equal(t, 1, sentCount)
}

func TestBroadcastExecute_NilSendMessage(t *testing.T) {
	userStore := newTestUserDB(t)
	ctx := context.Background()

	_, err := userStore.Create(ctx, 100, "user", "User", "user", "full", nil)
	require.NoError(t, err)

	a := &BroadcastAction{
		userStore: userStore,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err = a.Execute(ctx, "Test message")
	require.NoError(t, err)
}

func TestBroadcastExecute_EmptyMessage(t *testing.T) {
	a := &BroadcastAction{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := a.Execute(context.Background(), "")
	require.Error(t, err)
}
