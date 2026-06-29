package action

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/user"
)

var errLockFail = errors.New("lock failure")

type mockLockCommander struct {
	lockFunc    func(ctx context.Context) (bool, error)
	unlockFunc  func(ctx context.Context) (bool, error)
	lockCalls   int
	unlockCalls int
}

func (m *mockLockCommander) Lock(ctx context.Context) (bool, error) {
	m.lockCalls++
	if m.lockFunc != nil {
		return m.lockFunc(ctx)
	}
	return true, nil
}

func (m *mockLockCommander) Unlock(ctx context.Context) (bool, error) {
	m.unlockCalls++
	if m.unlockFunc != nil {
		return m.unlockFunc(ctx)
	}
	return true, nil
}

func newAccessLogDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE access_log (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			occurred_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_telegram_id INTEGER,
			action            TEXT NOT NULL,
			result            TEXT NOT NULL,
			detail            TEXT
		);
	`)
	require.NoError(t, err)
	return db
}

func activeUser(tid int64) *user.User {
	return &user.User{TelegramID: tid, IsActive: true, Tier: "full"}
}

func inactiveUser(tid int64) *user.User {
	return &user.User{TelegramID: tid, IsActive: false, Tier: "full"}
}

func expiredUser(tid int64) *user.User {
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	return &user.User{TelegramID: tid, IsActive: true, Tier: "full", ExpiresAt: &past}
}

func defaultCfg() Config {
	return Config{
		DaytimeStart:        "08:00",
		DaytimeEnd:          "18:00",
		UnlockMax:           5,
		UnlockWindowMinutes: 60,
	}
}

// --- UnlockAction denial tests ---

func TestUnlockAction_DeniedExpired(t *testing.T) {
	tests := []struct {
		name string
		user *user.User
	}{
		{name: "inactive", user: inactiveUser(1)},
		{name: "expired", user: expiredUser(2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newAccessLogDB(t)
			als := accesslog.New(db)
			mock := &mockLockCommander{}
			act := &UnlockAction{
				accessLog:  als,
				lockClient: mock,
				cfg:        defaultCfg(),
				log:        testLog,
			}

			result := act.Execute(context.Background(), tt.user)

			assert.False(t, result.Success)
			assert.Equal(t, 0, mock.unlockCalls, "should not call Unlock")

			entries, err := als.List(context.Background(), 10)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, "denied_expired", entries[0].Action)
			assert.Equal(t, "denied", entries[0].Result)
			assert.Equal(t, tt.user.TelegramID, *entries[0].ActorTelegramID)
		})
	}
}

func TestUnlockAction_DeniedTier(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{}
	act := &UnlockAction{
		accessLog:  als,
		lockClient: mock,
		cfg:        defaultCfg(),
		log:        testLog,
	}

	usr := &user.User{TelegramID: 1, IsActive: true, Tier: "basic"}
	result := act.Execute(context.Background(), usr)

	assert.False(t, result.Success)
	assert.Equal(t, 0, mock.unlockCalls, "should not call Unlock")

	entries, err := als.List(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "denied_tier", entries[0].Action)
	assert.Equal(t, "denied", entries[0].Result)
}

func TestUnlockAction_DeniedRateLimit(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{}
	act := &UnlockAction{
		accessLog:  als,
		lockClient: mock,
		cfg: Config{
			UnlockMax:           1,
			UnlockWindowMinutes: 60,
		},
		log: testLog,
	}

	ctx := context.Background()
	usr := activeUser(1)

	err := als.Write(ctx, accesslog.Entry{
		OccurredAt:      time.Now(),
		ActorTelegramID: &usr.TelegramID,
		Action:          "unlock",
		Result:          "success",
		Detail:          "",
	})
	require.NoError(t, err)

	result := act.Execute(ctx, usr)

	assert.False(t, result.Success)
	assert.Equal(t, 0, mock.unlockCalls, "should not call Unlock")

	entries, err := als.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "denied_rate_limit", entries[0].Action)
	assert.Equal(t, "denied", entries[0].Result)
}

// --- UnlockAction command outcome tests ---

func TestUnlockAction_Success(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{}
	act := &UnlockAction{
		accessLog:  als,
		lockClient: mock,
		cfg:        defaultCfg(),
		log:        testLog,
	}

	ctx := context.Background()
	result := act.Execute(ctx, activeUser(1))

	assert.True(t, result.Success)
	assert.Equal(t, 1, mock.unlockCalls)

	entries, err := als.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "unlock", entries[0].Action)
	assert.Equal(t, "success", entries[0].Result)
}

func TestUnlockAction_Timeout(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{
		unlockFunc: func(ctx context.Context) (bool, error) { return false, nil },
	}
	act := &UnlockAction{
		accessLog:  als,
		lockClient: mock,
		cfg:        defaultCfg(),
		log:        testLog,
	}

	ctx := context.Background()
	result := act.Execute(ctx, activeUser(1))

	assert.False(t, result.Success)
	assert.Equal(t, 1, mock.unlockCalls)

	entries, err := als.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "unlock", entries[0].Action)
	assert.Equal(t, "timeout", entries[0].Result)
}

func TestUnlockAction_Error(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{
		unlockFunc: func(ctx context.Context) (bool, error) { return false, errLockFail },
	}
	act := &UnlockAction{
		accessLog:  als,
		lockClient: mock,
		cfg:        defaultCfg(),
		log:        testLog,
	}

	ctx := context.Background()
	result := act.Execute(ctx, activeUser(1))

	assert.False(t, result.Success)
	assert.Equal(t, 1, mock.unlockCalls)

	entries, err := als.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "unlock", entries[0].Action)
	assert.Equal(t, "error", entries[0].Result)
	assert.Contains(t, entries[0].Detail, "lock failure")
}

// --- LockAction tests ---

func TestLockAction_DeniedExpired(t *testing.T) {
	tests := []struct {
		name string
		user *user.User
	}{
		{name: "inactive", user: inactiveUser(1)},
		{name: "expired", user: expiredUser(2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newAccessLogDB(t)
			als := accesslog.New(db)
			mock := &mockLockCommander{}
			act := &LockAction{
				accessLog:  als,
				lockClient: mock,
				log:        testLog,
			}

			result := act.Execute(context.Background(), tt.user)

			assert.False(t, result.Success)
			assert.Equal(t, 0, mock.lockCalls, "should not call Lock")

			entries, err := als.List(context.Background(), 10)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, "denied_expired", entries[0].Action)
			assert.Equal(t, "denied", entries[0].Result)
		})
	}
}

func TestLockAction_NotScheduleOrRateLimitGated(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)

	// Pre-seed rate limit entries that would block UnlockAction
	ctx := context.Background()
	usr := &user.User{TelegramID: 1, IsActive: true, Tier: "daytime"}
	for range 10 {
		err := als.Write(ctx, accesslog.Entry{
			OccurredAt:      time.Now(),
			ActorTelegramID: &usr.TelegramID,
			Action:          "unlock",
			Result:          "success",
			Detail:          "",
		})
		require.NoError(t, err)
	}

	mock := &mockLockCommander{}
	act := &LockAction{
		accessLog:  als,
		lockClient: mock,
		log:        testLog,
	}

	result := act.Execute(ctx, usr)

	assert.True(t, result.Success)
	assert.Equal(t, 1, mock.lockCalls)

	entries, err := als.List(ctx, 20)
	require.NoError(t, err)
	// Most recent entry is lock/success; no denied_tier or denied_rate_limit present
	assert.Equal(t, "lock", entries[0].Action)
	assert.Equal(t, "success", entries[0].Result)
	for _, e := range entries {
		assert.NotContains(t, e.Action, "denied_", "unexpected denial entry: %s/%s", e.Action, e.Result)
	}
}

func TestLockAction_Timeout(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{
		lockFunc: func(ctx context.Context) (bool, error) { return false, nil },
	}
	act := &LockAction{
		accessLog:  als,
		lockClient: mock,
		log:        testLog,
	}

	ctx := context.Background()
	result := act.Execute(ctx, activeUser(1))

	assert.False(t, result.Success)
	assert.Equal(t, 1, mock.lockCalls)

	entries, err := als.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "lock", entries[0].Action)
	assert.Equal(t, "timeout", entries[0].Result)
}

func TestLockAction_Error(t *testing.T) {
	db := newAccessLogDB(t)
	als := accesslog.New(db)
	mock := &mockLockCommander{
		lockFunc: func(ctx context.Context) (bool, error) { return false, errLockFail },
	}
	act := &LockAction{
		accessLog:  als,
		lockClient: mock,
		log:        testLog,
	}

	ctx := context.Background()
	result := act.Execute(ctx, activeUser(1))

	assert.False(t, result.Success)
	assert.Equal(t, 1, mock.lockCalls)

	entries, err := als.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "lock", entries[0].Action)
	assert.Equal(t, "error", entries[0].Result)
	assert.Contains(t, entries[0].Detail, "lock failure")
}
