package action

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// AlertConfig holds alert threshold configuration.
type AlertConfig struct {
	BatteryLowThreshold      int
	BatteryCriticalThreshold int
}

// DefaultAlertConfig returns an AlertConfig with default values.
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		BatteryLowThreshold:      20,
		BatteryCriticalThreshold: 10,
	}
}

// AlertManager subscribes to lock state changes and sends notifications.
type AlertManager struct {
	lockClient *lock.Client
	userStore  *user.Store
	accessLog  *accesslog.Store
	cfg        AlertConfig
	log        *slog.Logger

	// SendMessage is a callback that sends a message to a Telegram chat.
	// Set by the bot layer when wiring.
	SendMessage func(chatID int64, text string)

	mu                  sync.Mutex
	ready               bool
	batteryLowSent      bool
	batteryCriticalSent bool
}

// NewAlertManager creates an alert manager and subscribes to lock state changes.
func NewAlertManager(
	lockClient *lock.Client,
	userStore *user.Store,
	accessLog *accesslog.Store,
	cfg AlertConfig,
) *AlertManager {
	if cfg.BatteryLowThreshold <= 0 {
		cfg.BatteryLowThreshold = 20
	}
	if cfg.BatteryCriticalThreshold <= 0 {
		cfg.BatteryCriticalThreshold = 10
	}
	if cfg.BatteryCriticalThreshold > cfg.BatteryLowThreshold {
		cfg.BatteryLowThreshold, cfg.BatteryCriticalThreshold = cfg.BatteryCriticalThreshold, cfg.BatteryLowThreshold
	}

	return &AlertManager{
		lockClient: lockClient,
		userStore:  userStore,
		accessLog:  accessLog,
		cfg:        cfg,
		log:        slog.With("subsystem", "alerts"),
	}
}

// Start subscribes to lock state changes and begins processing alerts.
func (m *AlertManager) Start() {
	if m.lockClient == nil {
		m.log.Warn("lock client is nil, alerts not started")
		return
	}
	m.lockClient.SubscribeStateChanges(func(stateChange lock.StateChange) {
		m.handleStateChange(stateChange)
	})
	m.lockClient.SubscribeBattery(func(battery int) {
		m.handleBatteryUpdate(battery)
	})
	m.log.Info("alert manager started")
}

func (m *AlertManager) handleStateChange(stateChange lock.StateChange) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.ready {
		if stateChange.New.Lock != lock.StateUnknown || stateChange.New.Available {
			m.ready = true
		}
		return
	}

	if stateChange.New.Lock == lock.StateJammed {
		m.sendToAdmins("⚠️ Door jammed — check manually")
	}

	if stateChange.Old.Available && !stateChange.New.Available {
		m.sendToAdmins("❌ Lock went offline — manual check suggested")
	}

	if !stateChange.Old.Available && stateChange.New.Available && stateChange.New.Lock != lock.StateUnknown {
		m.sendToAdmins("✅ Lock reporting again")
	}

	m.checkBattery(stateChange.New.Battery)
}

func (m *AlertManager) handleBatteryUpdate(battery int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkBattery(battery)
}

func (m *AlertManager) checkBattery(battery int) {
	switch {
	case battery <= m.cfg.BatteryCriticalThreshold:
		if !m.batteryCriticalSent {
			m.batteryCriticalSent = true
			m.batteryLowSent = true
			m.sendToAdmins(fmt.Sprintf("🔋🔴 Battery critical: %d%%", battery))
		}
	case battery <= m.cfg.BatteryLowThreshold:
		if !m.batteryLowSent {
			m.batteryLowSent = true
			m.sendToAdmins(fmt.Sprintf("🔋 Battery low: %d%%", battery))
		}
	default:
		m.batteryLowSent = false
		m.batteryCriticalSent = false
	}
}

func (m *AlertManager) sendToAdmins(text string) {
	ctx := context.Background()
	users, err := m.userStore.List(ctx)
	if err != nil {
		m.log.Error("list users for alert", "error", err)
		return
	}
	for _, u := range users {
		if u.IsActive && u.Role == "admin" {
			m.sendMessage(u.TelegramID, text)
		}
	}
}

func (m *AlertManager) sendMessage(chatID int64, text string) {
	if m.SendMessage == nil {
		m.log.Warn("SendMessage callback not set", "text", text)
		return
	}
	m.SendMessage(chatID, text)
}
