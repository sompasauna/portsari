// Package lock provides lock-specific MQTT topic handling, state tracking, and
// command confirmation for the Yale L3 door lock.
//
// State changes arrive via MQTT from a HA blueprint that publishes JSON payloads.
// Commands are sent via the HA REST API.
package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/sompasauna/portsari/pkg/core/haapi"
	"github.com/sompasauna/portsari/pkg/core/mqtt"
)

// State represents the lock's known state.
type State string

// Standard lock states.
const (
	StateLocked   State = "locked"
	StateUnlocked State = "unlocked"
	StateJammed   State = "jammed"
	StateUnknown  State = "unknown"
)

// Config holds lock-specific configuration.
type Config struct {
	LockEntityID          string
	BatterySensorID       string
	StateTopicPrefix      string
	CommandConfirmTimeout time.Duration
}

func (c Config) validate() error {
	if c.LockEntityID == "" {
		return errors.New("lock: lock_entity_id is required")
	}
	if c.BatterySensorID == "" {
		return errors.New("lock: battery_sensor_id is required")
	}
	if c.StateTopicPrefix == "" {
		return errors.New("lock: state_topic_prefix is required")
	}
	if c.CommandConfirmTimeout <= 0 {
		return errors.New("lock: command_confirm_timeout must be positive")
	}
	return nil
}

// LockState represents the full lock state.
//
//nolint:revive // name required by spec; State is already the string enum type
type LockState struct {
	Lock      State
	Battery   int
	Available bool
}

// StateChange represents a lock state change event.
type StateChange struct {
	Old LockState
	New LockState
}

type watcher struct {
	ch     chan struct{}
	expect State
}

// blueprintPayload is the JSON structure published by the HA blueprint.
type blueprintPayload struct {
	EntityID   string          `json:"entity_id"`
	State      string          `json:"state"`
	OldState   *string         `json:"old_state"`
	Attributes json.RawMessage `json:"attributes"`
}

// Client wraps the generic MQTT client with lock-specific logic and uses
// the HA REST API for commands.
type Client struct {
	mqttClient *mqtt.Client
	haClient   haapi.Client
	cfg        Config
	log        *slog.Logger

	mu         sync.RWMutex
	state      LockState
	watchers   []watcher
	stateCbs   []func(StateChange)
	batteryCbs []func(int)

	closeMu sync.Mutex
	closed  bool
}

// New creates a lock Client using the given MQTT and HA API clients.
func New(mqttClient *mqtt.Client, haClient haapi.Client, cfg Config) *Client {
	if err := cfg.validate(); err != nil {
		slog.With("subsystem", "lock").Warn("invalid lock config", "error", err)
	}

	cli := &Client{
		mqttClient: mqttClient,
		haClient:   haClient,
		cfg:        cfg,
		log:        slog.With("subsystem", "lock"),
		state: LockState{
			Lock:      StateUnknown,
			Battery:   0,
			Available: false,
		},
	}

	if err := mqttClient.Subscribe(cfg.lockStateTopic(), func(msg mqtt.Message) {
		cli.handleLockState(string(msg.Payload))
	}); err != nil {
		cli.log.Error("subscribe lock state", "error", err)
	}

	if err := mqttClient.Subscribe(cfg.batteryTopic(), func(msg mqtt.Message) {
		cli.handleBattery(string(msg.Payload))
	}); err != nil {
		cli.log.Error("subscribe battery", "error", err)
	}

	return cli
}

// Lock sends a LOCK command via the HA REST API and waits for confirmation.
func (c *Client) Lock(ctx context.Context) (bool, error) {
	c.mu.RLock()
	if c.state.Lock == StateLocked && c.state.Available {
		c.mu.RUnlock()
		return true, nil
	}
	c.mu.RUnlock()

	ch := c.addWatcher(StateLocked)
	defer c.removeWatcher(ch)

	if err := c.haClient.Lock(ctx, c.cfg.LockEntityID); err != nil {
		return false, fmt.Errorf("lock: ha api: %w", err)
	}

	return c.waitForConfirm(ctx, ch)
}

// Unlock sends an UNLOCK command via the HA REST API and waits for confirmation.
func (c *Client) Unlock(ctx context.Context) (bool, error) {
	c.mu.RLock()
	if c.state.Lock == StateUnlocked && c.state.Available {
		c.mu.RUnlock()
		return true, nil
	}
	c.mu.RUnlock()

	ch := c.addWatcher(StateUnlocked)
	defer c.removeWatcher(ch)

	if err := c.haClient.Unlock(ctx, c.cfg.LockEntityID); err != nil {
		return false, fmt.Errorf("unlock: ha api: %w", err)
	}

	return c.waitForConfirm(ctx, ch)
}

func (c Config) lockStateTopic() string {
	return c.StateTopicPrefix + "/" + c.LockEntityID
}

func (c Config) batteryTopic() string {
	return c.StateTopicPrefix + "/" + c.BatterySensorID
}

func (c *Client) waitForConfirm(ctx context.Context, ch <-chan struct{}) (bool, error) {
	timer := time.NewTimer(c.cfg.CommandConfirmTimeout)
	defer timer.Stop()

	select {
	case <-ch:
		return true, nil
	case <-timer.C:
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// State returns the last known lock state.
func (c *Client) State() LockState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// SubscribeStateChanges registers a callback that is called on every state change.
func (c *Client) SubscribeStateChanges(callback func(StateChange)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateCbs = append(c.stateCbs, callback)
}

// SubscribeBattery registers a callback that is called on battery changes.
func (c *Client) SubscribeBattery(callback func(battery int)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batteryCbs = append(c.batteryCbs, callback)
}

// Close shuts down the lock client and its underlying MQTT connection.
func (c *Client) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.mqttClient.Close()
}

func (c *Client) addWatcher(expected State) chan struct{} {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.watchers = append(c.watchers, watcher{ch: ch, expect: expected})
	c.mu.Unlock()
	return ch
}

func (c *Client) removeWatcher(ch chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, w := range c.watchers {
		if w.ch == ch {
			c.watchers = append(c.watchers[:i], c.watchers[i+1:]...)
			break
		}
	}
}

func (c *Client) handleLockState(payload string) {
	var bp blueprintPayload
	if err := json.Unmarshal([]byte(payload), &bp); err != nil {
		c.log.Warn("invalid JSON payload on lock state topic", "payload", payload, "error", err)
		return
	}

	parsed, available, err := parseLockState(bp.State)
	if err != nil {
		c.log.Warn("unknown lock state in JSON payload", "state", bp.State)
		return
	}

	c.mu.Lock()
	old := c.state
	c.state.Lock = parsed
	c.state.Available = available
	newState := c.state

	for i := len(c.watchers) - 1; i >= 0; i-- {
		if c.watchers[i].expect == parsed {
			select {
			case c.watchers[i].ch <- struct{}{}:
			default:
			}
		}
	}
	c.mu.Unlock()

	if old.Lock != newState.Lock || old.Available != newState.Available {
		c.mu.RLock()
		cbs := make([]func(StateChange), len(c.stateCbs))
		copy(cbs, c.stateCbs)
		c.mu.RUnlock()
		for _, cb := range cbs {
			cb(StateChange{Old: old, New: newState})
		}
	}
}

func (c *Client) handleBattery(payload string) {
	var bp blueprintPayload
	if err := json.Unmarshal([]byte(payload), &bp); err != nil {
		c.log.Warn("invalid JSON payload on battery topic", "payload", payload, "error", err)
		return
	}

	val, err := parseBattery(bp.State)
	if err != nil {
		c.log.Warn("invalid battery percentage in JSON payload", "state", bp.State, "error", err)
		return
	}

	c.mu.Lock()
	old := c.state.Battery
	c.state.Battery = val
	if old != val {
		cbs := make([]func(int), len(c.batteryCbs))
		copy(cbs, c.batteryCbs)
		c.mu.Unlock()
		for _, cb := range cbs {
			cb(val)
		}
		return
	}
	c.mu.Unlock()
}

// parseLockState parses a state string from the blueprint JSON payload.
func parseLockState(state string) (State, bool, error) {
	switch state {
	case "locked":
		return StateLocked, true, nil
	case "unlocked":
		return StateUnlocked, true, nil
	case "jammed":
		return StateJammed, true, nil
	case "unavailable":
		return StateUnknown, false, nil
	default:
		return "", false, fmt.Errorf("lock: unknown state: %s", state)
	}
}

// parseBattery parses a battery percentage string from the blueprint JSON payload.
func parseBattery(state string) (int, error) {
	val, err := strconv.Atoi(state)
	if err != nil {
		return 0, fmt.Errorf("lock: parse battery: %w", err)
	}
	if val < 0 {
		val = 0
	} else if val > 100 {
		val = 100
	}
	return val, nil
}
