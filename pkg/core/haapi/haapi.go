// Package haapi provides a client for the Home Assistant REST API.
// Used by the lock package to send lock/unlock commands.
package haapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Client defines the interface for HA REST API operations.
type Client interface {
	// Lock sends a lock command for the given entity.
	Lock(ctx context.Context, entityID string) error
	// Unlock sends an unlock command for the given entity.
	Unlock(ctx context.Context, entityID string) error
}

type client struct {
	baseURL string
	token   string
	http    *http.Client
	log     *slog.Logger
}

// New creates a new HA API client.
func New(baseURL, token string) Client {
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: slog.With("subsystem", "haapi"),
	}
}

// Lock sends a lock command via the HA REST API.
func (c *client) Lock(ctx context.Context, entityID string) error {
	return c.callService(ctx, "lock/lock", entityID)
}

// Unlock sends an unlock command via the HA REST API.
func (c *client) Unlock(ctx context.Context, entityID string) error {
	return c.callService(ctx, "lock/unlock", entityID)
}

func (c *client) callService(ctx context.Context, service, entityID string) error {
	body, err := json.Marshal(map[string]string{"entity_id": entityID})
	if err != nil {
		return fmt.Errorf("haapi: marshal body: %w", err)
	}

	url := c.baseURL + "/api/services/" + service
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("haapi: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("haapi: %s: %w", service, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("haapi: %s: unexpected status %d", service, resp.StatusCode)
	}

	c.log.Debug("service call succeeded", "service", service, "entity_id", entityID)
	return nil
}
