package bot

import (
	"context"
	"errors"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/action"
	"github.com/sompasauna/portsari/pkg/core/accessrequest"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// RegisterUserHandlers registers user-facing command handlers on the bot.
func RegisterUserHandlers(b *Bot, actions *action.Actions, accessRequestStore *accessrequest.Store) {
	b.accessRequestStore = accessRequestStore
	b.RegisterActions(actions)
}

func (b *Bot) handleStart(msg *telego.Message, usr *user.User) {
	text := "Welcome to Portsari! Use the buttons below to control the door."
	markup := MainKeyboard()
	if usr.Role == RoleAdmin {
		markup = AdminKeyboard()
	}
	b.reply(msg.Chat.ID, text, markup)
}

func (b *Bot) handleUnlock(ctx context.Context, msg *telego.Message, usr *user.User) {
	b.log.Debug("unlock requested", "telegram_id", usr.TelegramID)
	result := b.actions.Unlock.Execute(ctx, usr)
	b.reply(msg.Chat.ID, result.Message)
}

func (b *Bot) handleLock(ctx context.Context, msg *telego.Message, usr *user.User) {
	b.log.Debug("lock requested", "telegram_id", usr.TelegramID)
	result := b.actions.Lock.Execute(ctx, usr)
	b.reply(msg.Chat.ID, result.Message)
}

func (b *Bot) handleHelp(msg *telego.Message) {
	text := `Available commands:
/unlock — Unlock the door
/lock — Lock the door
/help — Show this help message`
	b.reply(msg.Chat.ID, text)
}

// handleAccessRequest is called when an unknown user sends /start.
// It creates a pending access request and notifies all admins.
func (b *Bot) handleAccessRequest(msg *telego.Message, tgFrom *telego.User) {
	if b.accessRequestStore == nil {
		b.reply(msg.Chat.ID, "Access requests are not configured. Contact an admin.")
		return
	}
	ctx := context.Background()

	existing, err := b.accessRequestStore.GetLatestByTelegramID(ctx, tgFrom.ID)
	if err != nil && !errors.Is(err, accessrequest.ErrNotFound) {
		b.log.Error("check existing request", "error", err)
		b.reply(msg.Chat.ID, "Something went wrong. Please try again later.")
		return
	}
	if existing != nil && existing.Status == statusPending {
		b.reply(msg.Chat.ID, "Your access request is already pending. An admin will review it soon.")
		return
	}

	displayName := tgFrom.FirstName
	if tgFrom.LastName != "" {
		displayName = tgFrom.FirstName + " " + tgFrom.LastName
	}

	req, err := b.accessRequestStore.Create(ctx, tgFrom.ID, tgFrom.Username, displayName)
	if err != nil {
		b.log.Error("create access request", "error", err)
		b.reply(msg.Chat.ID, "Failed to submit request. Please try again later.")
		return
	}

	b.reply(msg.Chat.ID, "✅ Your access request has been submitted. An admin will review it soon.")
	b.notifyAdminsOfRequest(req)
}
