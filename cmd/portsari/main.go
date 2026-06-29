// Command portsari is the entry point for the portsari Telegram door bot.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sompasauna/portsari/internal/bot"
	"github.com/sompasauna/portsari/pkg/action"
	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/accessrequest"
	"github.com/sompasauna/portsari/pkg/core/config"
	"github.com/sompasauna/portsari/pkg/core/haapi"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/mqtt"
	"github.com/sompasauna/portsari/pkg/core/user"
	_ "modernc.org/sqlite"
)

func run() error {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("main: load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("subsystem", "main")
	slog.SetDefault(logger)

	logger.Info("config loaded", "path", *cfgPath)

	database, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("main: open database: %w", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := migrate(ctx, database); err != nil {
		return fmt.Errorf("main: migrate: %w", err)
	}

	logger.Info("database migrated", "path", cfg.Database.Path)

	userStore := user.New(database)
	accessLogStore := accesslog.New(database)
	accessRequestStore := accessrequest.New(database)

	mqttClient, err := mqtt.New(ctx, mqtt.Config{
		Broker:   cfg.MQTT.Broker,
		ClientID: cfg.MQTT.ClientID,
		Username: cfg.MQTT.Username,
		Password: cfg.MQTT.Password,
		QOS:      cfg.MQTT.QOS,
	})
	if err != nil {
		return fmt.Errorf("main: mqtt: %w", err)
	}
	defer mqttClient.Close()

	haClient := haapi.New(cfg.HAAPI.BaseURL, cfg.HAAPI.Token)

	lockClient := lock.New(mqttClient, haClient, lock.Config{
		LockEntityID:          cfg.HAAPI.EntityID,
		BatterySensorID:       "sensor." + cfg.Lock.EntitySlug + "_battery",
		StateTopicPrefix:      cfg.MQTT.StateTopicPrefix,
		CommandConfirmTimeout: time.Duration(cfg.Lock.CommandConfirmTimeoutSeconds) * time.Second,
	})

	actions := action.New(userStore, accessLogStore, lockClient, action.Config{
		Timezone:            cfg.Access.Timezone,
		DaytimeStart:        cfg.Access.DaytimeStart,
		DaytimeEnd:          cfg.Access.DaytimeEnd,
		UnlockMax:           cfg.RateLimit.UnlockMax,
		UnlockWindowMinutes: cfg.RateLimit.UnlockWindowMinute,
	})

	botInst, err := bot.New(cfg.Telegram.Token, os.Getenv("TELEGRAM_BOT_USER"), userStore, cfg.Access.BootstrapAdminTelegramID)
	if err != nil {
		return fmt.Errorf("main: bot: %w", err)
	}
	defer botInst.Stop()

	bot.RegisterUserHandlers(botInst, actions, accessRequestStore)
	bot.RegisterAdminHandlers(botInst, actions, accessLogStore, userStore, lockClient)

	actions.Broadcast.SendMessage = func(chatID int64, text string) {
		botInst.SendMessage(chatID, text)
	}

	alertManager := action.NewAlertManager(lockClient, userStore, accessLogStore, action.AlertConfig{
		BatteryLowThreshold:      cfg.Alerts.BatteryLowThreshold,
		BatteryCriticalThreshold: cfg.Alerts.BatteryCriticalThreshold,
	})
	alertManager.SendMessage = func(chatID int64, text string) {
		botInst.SendMessage(chatID, text)
	}
	alertManager.Start()

	logger.Info("bot started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.Info("shutting down", "signal", sig)

	logger.Info("bye")

	return nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func migrate(ctx context.Context, database *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
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
		)`,
		`CREATE TABLE IF NOT EXISTS invites (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			code              TEXT UNIQUE NOT NULL,
			created_by        INTEGER NOT NULL REFERENCES users (telegram_id),
			role              TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
			tier              TEXT NOT NULL DEFAULT 'full' CHECK (tier IN ('full', 'daytime')),
			grants_expires_at DATETIME,
			redeem_by         DATETIME,
			used_by           INTEGER,
			used_at           DATETIME,
			is_revoked        INTEGER NOT NULL DEFAULT 0,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS access_log (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			occurred_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_telegram_id   INTEGER,
			action              TEXT NOT NULL,
			result              TEXT NOT NULL,
			detail              TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS access_requests (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id  INTEGER NOT NULL,
			username     TEXT,
			display_name TEXT NOT NULL,
			requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected'))
		)`,
	}

	for _, q := range queries {
		if _, err := database.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("main: migrate: %w", err)
		}
	}

	return nil
}
