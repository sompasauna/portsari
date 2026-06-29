// Package config provides YAML configuration loading for the portsari bot.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MQTTConfig holds MQTT broker connection settings.
type MQTTConfig struct {
	Broker           string `yaml:"broker"`
	ClientID         string `yaml:"client_id"`
	Username         string `yaml:"username"`
	Password         string `yaml:"password"`
	StateTopicPrefix string `yaml:"state_topic_prefix"`
	QOS              int    `yaml:"qos"`
}

// HAAPIConfig holds Home Assistant REST API connection settings.
type HAAPIConfig struct {
	BaseURL  string `yaml:"base_url"`
	Token    string `yaml:"token"`
	EntityID string `yaml:"entity_id"`
}

// TelegramConfig holds the bot token.
type TelegramConfig struct {
	Token string `yaml:"token"`
}

// LockConfig holds lock-specific settings.
type LockConfig struct {
	EntitySlug                   string `yaml:"entity_slug"`
	CommandConfirmTimeoutSeconds int    `yaml:"command_confirm_timeout_seconds"`
}

// AccessConfig holds access control settings including optional timezone override.
type AccessConfig struct {
	Timezone                 string `yaml:"timezone"`
	DaytimeStart             string `yaml:"daytime_start"`
	DaytimeEnd               string `yaml:"daytime_end"`
	BootstrapAdminTelegramID int64  `yaml:"bootstrap_admin_telegram_id"`
}

// DatabaseConfig holds the SQLite database path.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// AlertsConfig holds battery threshold settings for proactive alerts.
type AlertsConfig struct {
	BatteryLowThreshold      int `yaml:"battery_low_threshold"`
	BatteryCriticalThreshold int `yaml:"battery_critical_threshold"`
}

// RateLimitConfig holds rate limiting settings for unlock actions.
type RateLimitConfig struct {
	UnlockMax          int `yaml:"unlock_max"`
	UnlockWindowMinute int `yaml:"unlock_window_minutes"`
}

// Config is the top-level configuration struct for portsari.
type Config struct {
	MQTT      MQTTConfig      `yaml:"mqtt"`
	HAAPI     HAAPIConfig     `yaml:"ha_api"`
	Telegram  TelegramConfig  `yaml:"telegram"`
	Lock      LockConfig      `yaml:"lock"`
	Access    AccessConfig    `yaml:"access"`
	Database  DatabaseConfig  `yaml:"database"`
	Alerts    AlertsConfig    `yaml:"alerts"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

// Load reads and parses a YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: load: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: load: %w", err)
	}

	return &cfg, nil
}
