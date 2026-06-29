//go:build integration

package mqtt

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntegration_ConnectPublishSubscribe(t *testing.T) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		t.Skip("set MQTT_BROKER environment variable to run this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := New(ctx, Config{
		Broker:   broker,
		ClientID: "portsari-integration-test",
		QOS:      1,
	})
	require.NoError(t, err)
	defer client.Close()

	received := make(chan Message, 1)
	err = client.Subscribe("test/portsari", func(msg Message) {
		received <- msg
	})
	require.NoError(t, err)

	err = client.Publish(ctx, "test/portsari", []byte("hello"))
	require.NoError(t, err)

	select {
	case msg := <-received:
		require.Equal(t, "test/portsari", msg.Topic)
		require.Equal(t, []byte("hello"), msg.Payload)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}
