package accesslog

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

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.ExecContext(context.Background(),
		`CREATE TABLE access_log (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			occurred_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_telegram_id   INTEGER,
			action              TEXT NOT NULL,
			result              TEXT NOT NULL,
			detail              TEXT
		)`)
	require.NoError(t, err)

	return New(db)
}

func TestStore_WriteAndList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tgID := int64(42)
	now := time.Now().UTC()
	entry := Entry{
		OccurredAt:      now,
		ActorTelegramID: &tgID,
		Action:          "unlock",
		Result:          "success",
		Detail:          "front door",
	}

	err := store.Write(ctx, entry)
	require.NoError(t, err)

	entries, err := store.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, entry.Action, entries[0].Action)
	assert.Equal(t, entry.Result, entries[0].Result)
	assert.Equal(t, entry.Detail, entries[0].Detail)
	assert.Equal(t, *entry.ActorTelegramID, *entries[0].ActorTelegramID)
	assert.WithinDuration(t, entry.OccurredAt, entries[0].OccurredAt, time.Second)
}

func TestStore_ListLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tgID := int64(99)
	now := time.Now().UTC()
	e1 := Entry{OccurredAt: now.Add(-1 * time.Hour), ActorTelegramID: &tgID, Action: "unlock", Result: "success", Detail: "old"}
	e2 := Entry{OccurredAt: now, ActorTelegramID: &tgID, Action: "lock", Result: "success", Detail: "new"}

	require.NoError(t, store.Write(ctx, e1))
	require.NoError(t, store.Write(ctx, e2))

	entries, err := store.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "new", entries[0].Detail)
}

func TestStore_RecentSuccessCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tgID := int64(77)
	now := time.Now().UTC()

	entries := []Entry{
		{OccurredAt: now.Add(-30 * time.Minute), ActorTelegramID: &tgID, Action: "unlock", Result: "success", Detail: "outside window"},
		{OccurredAt: now.Add(-5 * time.Minute), ActorTelegramID: &tgID, Action: "unlock", Result: "success", Detail: "inside window"},
		{OccurredAt: now.Add(-2 * time.Minute), ActorTelegramID: &tgID, Action: "unlock", Result: "denied", Detail: "wrong result"},
		{OccurredAt: now.Add(-1 * time.Minute), ActorTelegramID: &tgID, Action: "lock", Result: "success", Detail: "wrong action"},
	}
	for _, e := range entries {
		require.NoError(t, store.Write(ctx, e))
	}

	count, err := store.RecentSuccessCount(ctx, tgID, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestStore_RecentSuccessCount_otherUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	otherID := int64(111)
	matchID := int64(222)
	now := time.Now().UTC()

	require.NoError(t, store.Write(ctx, Entry{
		OccurredAt: now.Add(-5 * time.Minute), ActorTelegramID: &otherID,
		Action: "unlock", Result: "success",
	}))
	require.NoError(t, store.Write(ctx, Entry{
		OccurredAt: now.Add(-5 * time.Minute), ActorTelegramID: &matchID,
		Action: "unlock", Result: "success",
	}))

	count, err := store.RecentSuccessCount(ctx, matchID, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestStore_NilActorTelegramID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entry := Entry{
		OccurredAt:      time.Now().UTC(),
		ActorTelegramID: nil,
		Action:          "lock",
		Result:          "success",
		Detail:          "keypad entry",
	}

	err := store.Write(ctx, entry)
	require.NoError(t, err)

	entries, err := store.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Nil(t, entries[0].ActorTelegramID)
	assert.Equal(t, entry.Action, entries[0].Action)
	assert.Equal(t, entry.Result, entries[0].Result)
	assert.Equal(t, entry.Detail, entries[0].Detail)
}
