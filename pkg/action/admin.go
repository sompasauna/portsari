package action

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sompasauna/portsari/pkg/core/user"
)

// SetRoleAction updates a user's role.
type SetRoleAction struct {
	userStore *user.Store
	log       *slog.Logger
}

// Execute updates the role for the user identified by telegramID.
func (a *SetRoleAction) Execute(ctx context.Context, telegramID int64, role string) error {
	if err := a.userStore.SetRole(ctx, telegramID, role); err != nil {
		return fmt.Errorf("action: set role: %w", err)
	}
	return nil
}

// SetTierAction updates a user's tier.
type SetTierAction struct {
	userStore *user.Store
	log       *slog.Logger
}

// Execute updates the tier for the user identified by telegramID.
func (a *SetTierAction) Execute(ctx context.Context, telegramID int64, tier string) error {
	if err := a.userStore.SetTier(ctx, telegramID, tier); err != nil {
		return fmt.Errorf("action: set tier: %w", err)
	}
	return nil
}

// RemoveAction deactivates a user.
type RemoveAction struct {
	userStore *user.Store
	log       *slog.Logger
}

// Execute deactivates the user identified by telegramID.
func (a *RemoveAction) Execute(ctx context.Context, telegramID int64) error {
	if err := a.userStore.Deactivate(ctx, telegramID); err != nil {
		return fmt.Errorf("action: remove: %w", err)
	}
	return nil
}

// BroadcastAction sends a broadcast message to all active users.
type BroadcastAction struct {
	userStore   *user.Store
	log         *slog.Logger
	SendMessage func(chatID int64, text string)
}

// Execute sends the message to all active users.
func (a *BroadcastAction) Execute(ctx context.Context, message string) error {
	if message == "" {
		return errors.New("action: broadcast: empty message")
	}
	a.log.Info("broadcasting message", "message", message)

	users, err := a.userStore.List(ctx)
	if err != nil {
		return fmt.Errorf("action: broadcast: list users: %w", err)
	}

	sent := 0
	for _, u := range users {
		if u.IsActive {
			if a.SendMessage != nil {
				a.SendMessage(u.TelegramID, "📢 "+message)
			}
			sent++
		}
	}
	a.log.Info("broadcast sent", "recipients", sent)
	return nil
}
