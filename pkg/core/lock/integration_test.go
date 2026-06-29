//go:build integration

package lock

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sompasauna/portsari/pkg/core/haapi"
	"github.com/sompasauna/portsari/pkg/core/mqtt"
	"github.com/stretchr/testify/require"
)

func TestIntegration_LockStateTracking(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-lock-integration-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	slug := fmt.Sprintf("lock.la0010g_integration_%d", time.Now().UnixNano())
	lockClient := New(mqttClient, &mockHAClient{}, Config{
		LockEntityID:          slug,
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	})
	defer lockClient.Close()

	require.Equal(t, StateUnknown, lockClient.State().Lock)
	require.False(t, lockClient.State().Available)

	// Publish JSON payload matching the blueprint format
	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"locked","old_state":"unlocked","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		s := lockClient.State()
		return s.Lock == StateLocked && s.Available
	}, 2*time.Second, 50*time.Millisecond)

	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"unavailable","old_state":"locked","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		s := lockClient.State()
		return s.Lock == StateUnknown && !s.Available
	}, 2*time.Second, 50*time.Millisecond)
}

func TestIntegration_BatteryTracking(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-lock-battery-integration-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	slug := fmt.Sprintf("sensor.la0010g_battery_%d", time.Now().UnixNano())
	lockClient := New(mqttClient, &mockHAClient{}, Config{
		LockEntityID:          "lock.la0010g",
		BatterySensorID:       slug,
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	})
	defer lockClient.Close()

	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"85","old_state":"0","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return lockClient.State().Battery == 85
	}, 2*time.Second, 50*time.Millisecond)
}

func TestIntegration_ConcurrentSubscriptions(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-concurrent-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	topic1 := "portsari/test/topic1"
	topic2 := "portsari/test/topic2"

	recv1 := make(chan string, 1)
	recv2 := make(chan string, 1)

	err = mqttClient.Subscribe(topic1, func(msg mqtt.Message) {
		recv1 <- string(msg.Payload)
	})
	require.NoError(t, err)

	err = mqttClient.Subscribe(topic2, func(msg mqtt.Message) {
		recv2 <- string(msg.Payload)
	})
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	require.NoError(t, mqttClient.Publish(ctx, topic1, []byte("hello1")))
	require.NoError(t, mqttClient.Publish(ctx, topic2, []byte("hello2")))

	select {
	case payload := <-recv1:
		require.Equal(t, "hello1", payload)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message on topic1")
	}

	select {
	case payload := <-recv2:
		require.Equal(t, "hello2", payload)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message on topic2")
	}
}

func TestIntegration_LockCommandConfirm(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-lock-confirm-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	slug := fmt.Sprintf("lock.la0010g_confirm_%d", time.Now().UnixNano())

	mockHA := &mockHAClient{}
	lockClient := New(mqttClient, mockHA, Config{
		LockEntityID:          slug,
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	})
	defer lockClient.Close()

	// Set initial state to unlocked
	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"unlocked","old_state":"locked","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return lockClient.State().Lock == StateUnlocked && lockClient.State().Available
	}, 2*time.Second, 50*time.Millisecond)

	// Simulate HA API responding by publishing the state change
	go func() {
		time.Sleep(200 * time.Millisecond)
		mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
			`{"entity_id":"`+slug+`","state":"locked","old_state":"unlocked","attributes":{}}`))
	}()

	confirmed, err := lockClient.Lock(ctx)
	require.NoError(t, err)
	require.True(t, confirmed, "Lock should confirm when matching state update arrives")
	require.Equal(t, 1, mockHA.lockCalls, "HA API should have been called")
}

func TestIntegration_UnlockCommandConfirm(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-unlock-confirm-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	slug := fmt.Sprintf("lock.la0010g_uconfirm_%d", time.Now().UnixNano())

	mockHA := &mockHAClient{}
	lockClient := New(mqttClient, mockHA, Config{
		LockEntityID:          slug,
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 5 * time.Second,
	})
	defer lockClient.Close()

	// Set initial state to locked
	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"locked","old_state":"unlocked","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return lockClient.State().Lock == StateLocked && lockClient.State().Available
	}, 2*time.Second, 50*time.Millisecond)

	go func() {
		time.Sleep(200 * time.Millisecond)
		mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
			`{"entity_id":"`+slug+`","state":"unlocked","old_state":"locked","attributes":{}}`))
	}()

	confirmed, err := lockClient.Unlock(ctx)
	require.NoError(t, err)
	require.True(t, confirmed, "Unlock should confirm when matching state update arrives")
	require.Equal(t, 1, mockHA.unlockCalls, "HA API should have been called")
}

func TestIntegration_LockCommandTimeout(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-lock-timeout-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	slug := fmt.Sprintf("lock.la0010g_timeout_%d", time.Now().UnixNano())

	mockHA := &mockHAClient{}
	lockClient := New(mqttClient, mockHA, Config{
		LockEntityID:          slug,
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 1 * time.Second,
	})
	defer lockClient.Close()

	// Set initial state to unlocked
	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"unlocked","old_state":"locked","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return lockClient.State().Lock == StateUnlocked && lockClient.State().Available
	}, 2*time.Second, 50*time.Millisecond)

	// Don't publish a confirmation — should timeout
	confirmed, err := lockClient.Lock(ctx)
	require.NoError(t, err)
	require.False(t, confirmed, "Lock should timeout when no confirmation arrives")
	require.Equal(t, 1, mockHA.lockCalls, "HA API should have been called")
}

func TestIntegration_UnlockCommandTimeout(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER to run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   broker,
		ClientID: "portsari-unlock-timeout-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer mqttClient.Close()

	slug := fmt.Sprintf("lock.la0010g_utimeout_%d", time.Now().UnixNano())

	mockHA := &mockHAClient{}
	lockClient := New(mqttClient, mockHA, Config{
		LockEntityID:          slug,
		BatterySensorID:       "sensor.la0010g_battery",
		StateTopicPrefix:      "ha/varasto",
		CommandConfirmTimeout: 1 * time.Second,
	})
	defer lockClient.Close()

	// Set initial state to locked
	err = mqttClient.Publish(ctx, "ha/varasto/"+slug, []byte(
		`{"entity_id":"`+slug+`","state":"locked","old_state":"unlocked","attributes":{}}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return lockClient.State().Lock == StateLocked && lockClient.State().Available
	}, 2*time.Second, 50*time.Millisecond)

	confirmed, err := lockClient.Unlock(ctx)
	require.NoError(t, err)
	require.False(t, confirmed, "Unlock should timeout when no confirmation arrives")
	require.Equal(t, 1, mockHA.unlockCalls, "HA API should have been called")
}
