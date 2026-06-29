package mqtt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_EmptyBroker(t *testing.T) {
	_, err := New(context.Background(), Config{
		ClientID: "test-client",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker is required")
}

func TestNew_EmptyBrokerCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(ctx, Config{
		ClientID: "test-client",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker is required")
}
