// Package mqtt provides a thin wrapper around the Eclipse Paho MQTT client
// for broker connection, subscription, and publication.
package mqtt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const maxQOS = 2

// Config holds MQTT broker connection settings.
type Config struct {
	Broker   string
	ClientID string
	Username string
	Password string
	QOS      int
}

func (c Config) validate() error {
	if c.Broker == "" {
		return errors.New("mqtt: broker is required")
	}
	if c.QOS < 0 || c.QOS > maxQOS {
		return fmt.Errorf("mqtt: QOS %d out of range [0,%d]", c.QOS, maxQOS)
	}
	return nil
}

// Message represents an MQTT message received on a subscribed topic.
type Message struct {
	Topic   string
	Payload []byte
}

// MessageHandler is called for every message received on a subscribed topic.
type MessageHandler func(msg Message)

// Client is a connected MQTT client.
type Client struct {
	client mqtt.Client
	mu     sync.Mutex
	done   chan struct{}
	log    *slog.Logger
	qos    byte
}

// New creates and connects a new MQTT client. Blocks until the connection
// is established or ctx is cancelled.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if cfg.QOS == 0 {
		cfg.QOS = 1
	}

	log := slog.With("subsystem", "mqtt")

	opts := mqtt.NewClientOptions().
		SetClientID(cfg.ClientID).
		SetAutoReconnect(true).
		SetCleanSession(true).
		SetOnConnectHandler(func(_ mqtt.Client) {
			log.Info("connected to MQTT broker")
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Error("MQTT connection lost", "error", err)
		})

	if cfg.Broker != "" {
		opts.AddBroker(cfg.Broker)
	}
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	cli := mqtt.NewClient(opts)
	token := cli.Connect()

	errCh := make(chan error, 1)
	go func() {
		token.Wait()
		errCh <- token.Error()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			cli.Disconnect(250)
			return nil, fmt.Errorf("mqtt: connect: %w", err)
		}
	case <-ctx.Done():
		token.Wait()
		cli.Disconnect(250)
		return nil, ctx.Err()
	}

	log.Info("MQTT client created", "broker", cfg.Broker, "client_id", cfg.ClientID)

	if cfg.QOS < 0 || cfg.QOS > maxQOS {
		return nil, fmt.Errorf("mqtt: QOS %d out of range [0,%d]", cfg.QOS, maxQOS)
	}

	return &Client{
		client: cli,
		log:    log,
		done:   make(chan struct{}),
		qos:    byte(cfg.QOS),
	}, nil
}

// Subscribe registers a handler for the given topic. The handler is called
// for every message received on this topic.
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	token := c.client.Subscribe(topic, c.qos, func(_ mqtt.Client, msg mqtt.Message) {
		handler(Message{
			Topic:   msg.Topic(),
			Payload: msg.Payload(),
		})
	})
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt: subscribe: %w", err)
	}
	return nil
}

// Publish sends a payload to the given topic with the configured QOS.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	token := c.client.Publish(topic, c.qos, false, payload)

	errCh := make(chan error, 1)
	go func() {
		token.Wait()
		errCh <- token.Error()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("mqtt: publish: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqtt: publish: %w", ctx.Err())
	}
}

// Done returns a channel that is closed when the client is permanently disconnected
// via Close.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close disconnects from the broker and cleans up resources.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.done:
		return nil
	default:
		close(c.done)
	}

	c.client.Disconnect(250)
	c.log.Info("MQTT client disconnected")
	return nil
}
