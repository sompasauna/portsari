package action

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/user"
)

type testMsg struct {
	chatID int64
	text   string
}

type testSender struct {
	mu       sync.Mutex
	messages []testMsg
}

func (s *testSender) send(chatID int64, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, testMsg{chatID: chatID, text: text})
}

func (s *testSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func newAlertTestDB(t *testing.T) *sql.DB {
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
		);
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

func setupAlertTest(t *testing.T) (*AlertManager, *testSender, *user.Store) {
	t.Helper()

	db := newAlertTestDB(t)
	us := user.New(db)
	als := accesslog.New(db)

	ctx := context.Background()

	_, err := us.Create(ctx, 100, "admin1", "Admin One", "admin", "full", nil)
	require.NoError(t, err)
	_, err = us.Create(ctx, 101, "admin2", "Admin Two", "admin", "full", nil)
	require.NoError(t, err)
	_, err = us.Create(ctx, 200, "user1", "User One", "user", "full", nil)
	require.NoError(t, err)

	sender := &testSender{}
	am := &AlertManager{
		userStore:   us,
		accessLog:   als,
		cfg:         DefaultAlertConfig(),
		log:         testLog,
		SendMessage: sender.send,
	}

	return am, sender, us
}

var testLog = slog.With("subsystem", "test")

func TestAlertManager_InitialUnknown_NoAlerts(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	sc := lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 0, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	}
	am.handleStateChange(sc)

	assert.Equal(t, 0, sender.count(), "no alerts on initial catch-up")

	sc2 := lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		New: lock.LockState{Lock: lock.StateUnlocked, Battery: 50, Available: true},
	}
	am.handleStateChange(sc2)

	assert.Equal(t, 0, sender.count(), "no alert on external unlock")
}

func TestAlertManager_LockUnlockNoAlerts(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// Prime the ready flag
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	t.Run("locked to unlocked sends no alert", func(t *testing.T) {
		am.handleStateChange(lock.StateChange{
			Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
			New: lock.LockState{Lock: lock.StateUnlocked, Battery: 50, Available: true},
		})
		assert.Equal(t, 0, sender.count())
	})

	t.Run("unlocked to locked sends no alert", func(t *testing.T) {
		am.handleStateChange(lock.StateChange{
			Old: lock.LockState{Lock: lock.StateUnlocked, Battery: 50, Available: true},
			New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		})
		assert.Equal(t, 0, sender.count())
	})
}

func TestAlertManager_JammedAlert(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// Prime
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		New: lock.LockState{Lock: lock.StateJammed, Battery: 50, Available: true},
	})

	assert.Equal(t, 2, sender.count(), "jammed alert sent to admins only")

	sender.mu.Lock()
	defer sender.mu.Unlock()
	for _, msg := range sender.messages {
		assert.Contains(t, msg.text, "⚠️")
		assert.Contains(t, msg.text, "jammed")
	}
}

func TestAlertManager_OfflineOnlineAlerts(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// Prime
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	t.Run("unavailable alerts admins", func(t *testing.T) {
		am.handleStateChange(lock.StateChange{
			Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
			New: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		})
		assert.Equal(t, 2, sender.count())
		assert.Contains(t, sender.messages[0].text, "❌")
		sender.mu.Lock()
		sender.messages = nil
		sender.mu.Unlock()
	})

	t.Run("back online alerts admins", func(t *testing.T) {
		am.handleStateChange(lock.StateChange{
			Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
			New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		})
		sender.mu.Lock()
		assert.Equal(t, 2, len(sender.messages))
		hasBackOnline := false
		for _, msg := range sender.messages {
			if msg.text == "✅ Lock reporting again" {
				hasBackOnline = true
			}
		}
		sender.mu.Unlock()
		assert.True(t, hasBackOnline, "expected back online message")
		sender.mu.Lock()
		sender.messages = nil
		sender.mu.Unlock()
	})
}

func TestAlertManager_BatteryThresholds(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// Prime
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	t.Run("low battery alerts admins", func(t *testing.T) {
		am.handleStateChange(lock.StateChange{
			Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
			New: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
		})
		assert.Equal(t, 2, sender.count(), "low battery alert to admins")
		for _, msg := range sender.messages {
			assert.Contains(t, msg.text, "🔋")
			assert.Contains(t, msg.text, "15%")
		}
		sender.mu.Lock()
		sender.messages = nil
		sender.mu.Unlock()
	})

	t.Run("critical battery alerts admins", func(t *testing.T) {
		am.handleStateChange(lock.StateChange{
			Old: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
			New: lock.LockState{Lock: lock.StateLocked, Battery: 8, Available: true},
		})
		assert.Equal(t, 2, sender.count(), "critical battery alert to admins")
		for _, msg := range sender.messages {
			assert.Contains(t, msg.text, "🔋🔴")
			assert.Contains(t, msg.text, "8%")
		}
		sender.mu.Lock()
		sender.messages = nil
		sender.mu.Unlock()
	})
}

func TestAlertManager_DuplicateBatteryAlerts(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// Prime
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	// Trigger low battery
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
	})
	assert.Equal(t, 2, sender.count(), "first low battery alert")
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	// Same battery level again — no duplicate
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
	})
	assert.Equal(t, 0, sender.count(), "no duplicate low battery alert")

	// Battery recovers above threshold
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	assert.Equal(t, 0, sender.count(), "no alert on recovery")

	// Battery drops again — should re-alert
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 18, Available: true},
	})
	assert.Equal(t, 2, sender.count(), "re-alert after recovery")
}

func TestAlertManager_ReadyFlagNotSetFromUnavailable(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// First callback: unavailable (Unknown, false) -> (Unknown, false) — but this
	// won't fire from lock client since Lock and Available don't change.
	// Simulate the case where Available changes to false.
	sc := lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 0, Available: false},
		New: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
	}
	am.handleStateChange(sc)

	// Still not ready — Lock didn't change to a real state
	assert.False(t, am.ready, "should not be ready after unavailable-only change")

	// Now real state arrives
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	assert.True(t, am.ready, "should be ready after real state")
	assert.Equal(t, 0, sender.count(), "no alerts during catch-up")
}

func TestDefaultAlertConfig(t *testing.T) {
	cfg := DefaultAlertConfig()
	assert.Equal(t, 20, cfg.BatteryLowThreshold)
	assert.Equal(t, 10, cfg.BatteryCriticalThreshold)
}

func TestNewAlertManager_SwapsThresholds(t *testing.T) {
	cfg := AlertConfig{BatteryLowThreshold: 5, BatteryCriticalThreshold: 15}
	db := newAlertTestDB(t)
	am := NewAlertManager(nil, user.New(db), accesslog.New(db), cfg)
	assert.Equal(t, 15, am.cfg.BatteryLowThreshold)
	assert.Equal(t, 5, am.cfg.BatteryCriticalThreshold)
}

func TestNewAlertManager_DefaultZeroThresholds(t *testing.T) {
	db := newAlertTestDB(t)
	am := NewAlertManager(nil, user.New(db), accesslog.New(db), AlertConfig{})
	assert.Equal(t, 20, am.cfg.BatteryLowThreshold)
	assert.Equal(t, 10, am.cfg.BatteryCriticalThreshold)
}

func TestAlertManager_InactiveUserNoAlert(t *testing.T) {
	db := newAlertTestDB(t)
	us := user.New(db)
	als := accesslog.New(db)
	ctx := context.Background()

	_, err := us.Create(ctx, 100, "admin1", "Admin One", "admin", "full", nil)
	require.NoError(t, err)

	err = us.Deactivate(ctx, 100)
	require.NoError(t, err)

	sender := &testSender{}
	am := &AlertManager{
		userStore:   us,
		accessLog:   als,
		cfg:         DefaultAlertConfig(),
		log:         testLog,
		SendMessage: sender.send,
	}

	// Prime
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	// Low battery should not go to inactive admin
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 15, Available: true},
	})
	assert.Equal(t, 0, sender.count(), "no alert for inactive user")
}

func TestAlertManager_BatteryUpdateDirect(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// Prime ready normally
	am.handleStateChange(lock.StateChange{
		Old: lock.LockState{Lock: lock.StateUnknown, Battery: 50, Available: false},
		New: lock.LockState{Lock: lock.StateLocked, Battery: 50, Available: true},
	})
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	am.handleBatteryUpdate(15)
	assert.Equal(t, 2, sender.count(), "low battery alert to admins")
	for _, msg := range sender.messages {
		assert.Contains(t, msg.text, "🔋 Battery low: 15%")
	}
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	am.handleBatteryUpdate(15)
	assert.Equal(t, 0, sender.count(), "no duplicate alert")
}

func TestAlertManager_BatteryUpdateBeforeReady(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	// NOT priming ready — should still fire battery alerts
	am.handleBatteryUpdate(8)
	assert.Equal(t, 2, sender.count(), "critical battery alert before ready")
	for _, msg := range sender.messages {
		assert.Contains(t, msg.text, "🔋🔴 Battery critical: 8%")
	}
}

func TestAlertManager_BatteryUpdateRecoveryAndReAlert(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	am.handleBatteryUpdate(15)
	assert.Equal(t, 2, sender.count(), "first low battery alert")
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	// Recovery above low threshold — resets flags
	am.handleBatteryUpdate(85)
	assert.Equal(t, 0, sender.count(), "no alert on recovery")
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	// Drop again — re-alert
	am.handleBatteryUpdate(18)
	assert.Equal(t, 2, sender.count(), "re-alert after recovery")
}

func TestAlertManager_BatteryUpdateRepeatedIdenticalNoDuplicate(t *testing.T) {
	am, sender, _ := setupAlertTest(t)

	am.handleBatteryUpdate(15)
	assert.Equal(t, 2, sender.count(), "first low battery alert")
	sender.mu.Lock()
	sender.messages = nil
	sender.mu.Unlock()

	am.handleBatteryUpdate(15)
	assert.Equal(t, 0, sender.count(), "no duplicate alert")
}

func TestAlertManager_StartWithNilLockClient(t *testing.T) {
	am := &AlertManager{
		log: testLog,
	}
	am.Start() // should not panic
}
