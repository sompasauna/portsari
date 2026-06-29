package lock

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/sompasauna/portsari/pkg/core/haapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHAClient struct {
	lockFunc   func(ctx context.Context, entityID string) error
	unlockFunc func(ctx context.Context, entityID string) error
	lockCalls  int
	unlockCalls int
}

func (m *mockHAClient) Lock(ctx context.Context, entityID string) error {
	m.lockCalls++
	if m.lockFunc != nil {
		return m.lockFunc(ctx, entityID)
	}
	return nil
}

func (m *mockHAClient) Unlock(ctx context.Context, entityID string) error {
	m.unlockCalls++
	if m.unlockFunc != nil {
		return m.unlockFunc(ctx, entityID)
	}
	return nil
}

var _ haapi.Client = (*mockHAClient)(nil)

func TestConfig_Validate_EmptyLockEntityID(t *testing.T) {
	cfg := Config{
		LockEntityID:          "",
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	}
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock_entity_id")
}

func TestConfig_Validate_EmptyBatterySensorID(t *testing.T) {
	cfg := Config{
		LockEntityID:          "lock.la0010g",
		BatterySensorID:       "",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	}
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "battery_sensor_id")
}

func TestConfig_Validate_ZeroTimeout(t *testing.T) {
	cfg := Config{
		LockEntityID:          "lock.la0010g",
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 0,
	}
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command_confirm_timeout")
}

func TestConfig_Validate_NegativeTimeout(t *testing.T) {
	cfg := Config{
		LockEntityID:          "lock.la0010g",
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: -1 * time.Second,
	}
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command_confirm_timeout")
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := Config{
		LockEntityID:          "lock.la0010g",
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	}
	err := cfg.validate()
	require.NoError(t, err)
}

func TestConfig_Validate_EmptyPrefix(t *testing.T) {
	cfg := Config{
		LockEntityID:          "lock.la0010g",
		BatterySensorID:       "sensor.la0010g_battery",
		CommandConfirmTimeout: 5 * time.Second,
	}
	err := cfg.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state_topic_prefix")
}

func TestConfigTopics(t *testing.T) {
	cfg := Config{
		LockEntityID:     "lock.la0010g",
		BatterySensorID:  "sensor.la0010g_battery",
		StateTopicPrefix: "ha/varasto",
	}

	assert.Equal(t, "ha/varasto/lock.la0010g", cfg.lockStateTopic())
	assert.Equal(t, "ha/varasto/sensor.la0010g_battery", cfg.batteryTopic())
}

func TestParseLockState(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		wantState State
		wantAvail bool
		wantErr   bool
	}{
		{name: "locked", state: "locked", wantState: StateLocked, wantAvail: true},
		{name: "unlocked", state: "unlocked", wantState: StateUnlocked, wantAvail: true},
		{name: "jammed", state: "jammed", wantState: StateJammed, wantAvail: true},
		{name: "unavailable", state: "unavailable", wantState: StateUnknown, wantAvail: false},
		{name: "unknown string", state: "unknown", wantErr: true},
		{name: "bogus", state: "bogus", wantErr: true},
		{name: "empty", state: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, avail, err := parseLockState(tt.state)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantState, s)
			assert.Equal(t, tt.wantAvail, avail)
		})
	}
}

func TestParseBattery(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		wantValue int
		wantErr   bool
	}{
		{name: "normal", state: "85", wantValue: 85},
		{name: "zero", state: "0", wantValue: 0},
		{name: "one hundred", state: "100", wantValue: 100},
		{name: "negative clamped", state: "-1", wantValue: 0},
		{name: "over max clamped", state: "101", wantValue: 100},
		{name: "non-numeric", state: "abc", wantErr: true},
		{name: "empty", state: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := parseBattery(tt.state)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantValue, val)
		})
	}
}

func TestBatteryCallbackFiresOnChange(t *testing.T) {
	cli := &Client{
		cfg: Config{
			LockEntityID:          "lock.la0010g",
			BatterySensorID:       "sensor.la0010g_battery",
			StateTopicPrefix:      "ha/varasto",
			CommandConfirmTimeout: time.Second,
		},
		log:   slog.With("subsystem", "lock"),
		state: LockState{Battery: 0},
	}

	var calls []int
	cli.SubscribeBattery(func(val int) {
		calls = append(calls, val)
	})

	cli.handleBattery(`{"entity_id":"sensor.la0010g_battery","state":"85","old_state":"0","attributes":{}}`)
	assert.Equal(t, []int{85}, calls)

	cli.handleBattery(`{"entity_id":"sensor.la0010g_battery","state":"50","old_state":"85","attributes":{}}`)
	assert.Equal(t, []int{85, 50}, calls)
}

func TestBatteryCallbackNoFireOnSameValue(t *testing.T) {
	cli := &Client{
		cfg: Config{
			LockEntityID:          "lock.la0010g",
			BatterySensorID:       "sensor.la0010g_battery",
			StateTopicPrefix:      "ha/varasto",
			CommandConfirmTimeout: time.Second,
		},
		log:   slog.With("subsystem", "lock"),
		state: LockState{Battery: 50},
	}

	var calls []int
	cli.SubscribeBattery(func(val int) {
		calls = append(calls, val)
	})

	cli.handleBattery(`{"entity_id":"sensor.la0010g_battery","state":"50","old_state":"50","attributes":{}}`)
	assert.Empty(t, calls)

	cli.handleBattery(`{"entity_id":"sensor.la0010g_battery","state":"85","old_state":"50","attributes":{}}`)
	assert.Equal(t, []int{85}, calls)

	cli.handleBattery(`{"entity_id":"sensor.la0010g_battery","state":"85","old_state":"85","attributes":{}}`)
	assert.Equal(t, []int{85}, calls)
}

func TestHandleLockStateJSON(t *testing.T) {
	cli := &Client{
		cfg: Config{
			LockEntityID:          "lock.la0010g",
			BatterySensorID:       "sensor.la0010g_battery",
			StateTopicPrefix:      "ha/varasto",
			CommandConfirmTimeout: time.Second,
		},
		log:   slog.With("subsystem", "lock"),
		state: LockState{Lock: StateUnknown, Battery: 0, Available: false},
	}

	// JSON payload from the blueprint
	cli.handleLockState(`{"entity_id":"lock.la0010g","state":"locked","old_state":"unlocked","attributes":{"friendly_name":"Etuovi"}}`)
	assert.Equal(t, StateLocked, cli.State().Lock)
	assert.True(t, cli.State().Available)

	cli.handleLockState(`{"entity_id":"lock.la0010g","state":"unlocked","old_state":"locked","attributes":{}}`)
	assert.Equal(t, StateUnlocked, cli.State().Lock)
	assert.True(t, cli.State().Available)

	cli.handleLockState(`{"entity_id":"lock.la0010g","state":"unavailable","old_state":"locked","attributes":{}}`)
	assert.Equal(t, StateUnknown, cli.State().Lock)
	assert.False(t, cli.State().Available)
}

func TestHandleLockStateInvalidJSON(t *testing.T) {
	cli := &Client{
		cfg: Config{
			LockEntityID:          "lock.la0010g",
			BatterySensorID:       "sensor.la0010g_battery",
			StateTopicPrefix:      "ha/varasto",
			CommandConfirmTimeout: time.Second,
		},
		log:   slog.With("subsystem", "lock"),
		state: LockState{Lock: StateLocked, Battery: 50, Available: true},
	}

	// Invalid JSON should not change state
	cli.handleLockState("not json")
	assert.Equal(t, StateLocked, cli.State().Lock)

	// Valid JSON but unknown state
	cli.handleLockState(`{"entity_id":"lock.la0010g","state":"bogus","old_state":"locked","attributes":{}}`)
	assert.Equal(t, StateLocked, cli.State().Lock)
}

func TestLockAlreadyLocked(t *testing.T) {
	mockHA := &mockHAClient{}
	cli := &Client{
		haClient: mockHA,
		cfg: Config{
			LockEntityID:          "lock.la0010g",
			BatterySensorID:       "sensor.la0010g_battery",
			StateTopicPrefix:      "ha/varasto",
			CommandConfirmTimeout: time.Second,
		},
		log:   slog.With("subsystem", "lock"),
		state: LockState{Lock: StateLocked, Battery: 50, Available: true},
	}

	confirmed, err := cli.Lock(context.Background())
	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, 0, mockHA.lockCalls, "should not call HA API when already locked")
}

func TestUnlockAlreadyUnlocked(t *testing.T) {
	mockHA := &mockHAClient{}
	cli := &Client{
		haClient: mockHA,
		cfg: Config{
			LockEntityID:          "lock.la0010g",
			BatterySensorID:       "sensor.la0010g_battery",
			StateTopicPrefix:      "ha/varasto",
			CommandConfirmTimeout: time.Second,
		},
		log:   slog.With("subsystem", "lock"),
		state: LockState{Lock: StateUnlocked, Battery: 50, Available: true},
	}

	confirmed, err := cli.Unlock(context.Background())
	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, 0, mockHA.unlockCalls, "should not call HA API when already unlocked")
}
