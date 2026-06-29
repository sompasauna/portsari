// Package bot provides the Telegram bot infrastructure for portsari.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/action"
	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/accessrequest"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// Bot wraps the telego bot and application wiring.
type Bot struct {
	api                *telego.Bot
	username           string
	userStore          *user.Store
	actions            *action.Actions
	lockClient         *lock.Client
	log                *slog.Logger
	cancel             context.CancelFunc
	bootstrapAdminID   int64
	accessLog          *accesslog.Store
	accessRequestStore *accessrequest.Store

	messageHandlers  map[string]func(context.Context, *telego.Message, *user.User, []string)
	callbackHandlers map[string]func(telego.Update, *user.User)

	mu               sync.Mutex
	broadcastPending map[int64]bool

	testEdit  func(chatID int64, messageID int, text string, markup *telego.InlineKeyboardMarkup)
	testReply func(chatID int64, text string, markup ...telego.ReplyMarkup)
}

// Config holds the bot-specific configuration.
type Config struct {
	Token string
}

// New creates and starts the bot.
func New(token, username string, userStore *user.Store, bootstrapAdminID int64) (*Bot, error) {
	api, err := telego.NewBot(token, telego.WithDefaultDebugLogger())
	if err != nil {
		return nil, fmt.Errorf("bot: new: %w", err)
	}

	botInst := &Bot{
		api:              api,
		username:         username,
		userStore:        userStore,
		bootstrapAdminID: bootstrapAdminID,
		log:              slog.With("subsystem", "telegram"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	botInst.cancel = cancel

	updates, err := api.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("bot: long polling: %w", err)
	}

	go botInst.processUpdates(ctx, updates)

	return botInst, nil
}

// Stop gracefully stops the bot's long polling loop.
func (b *Bot) Stop() {
	b.cancel()
}

// SendMessage sends a text message to the given chat.
func (b *Bot) SendMessage(chatID int64, text string) {
	b.reply(chatID, text)
}

func (b *Bot) setPendingBroadcast(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.broadcastPending == nil {
		b.broadcastPending = make(map[int64]bool)
	}
	b.broadcastPending[chatID] = true
}

func (b *Bot) hasPendingBroadcast(chatID int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.broadcastPending[chatID]
}

func (b *Bot) clearPendingBroadcast(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.broadcastPending, chatID)
}

// RegisterActions sets the action handlers on the bot.
func (b *Bot) RegisterActions(actions *action.Actions) {
	b.actions = actions
}

func (b *Bot) processUpdates(ctx context.Context, updates <-chan telego.Update) {
	b.log.Info("started polling")
	for {
		select {
		case <-ctx.Done():
			b.log.Info("polling stopped")
			return
		case update, ok := <-updates:
			if !ok {
				b.log.Info("updates channel closed")
				return
			}
			b.handleUpdate(update)
		}
	}
}

func (b *Bot) handleUpdate(update telego.Update) {
	if update.Message != nil && update.Message.Text != "" {
		b.handleMessage(update)
		return
	}
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(update)
		return
	}
}

func (b *Bot) handleMessage(update telego.Update) {
	if b.actions == nil {
		b.log.Warn("actions not set, ignoring message")
		return
	}

	msg := update.Message
	ctx := context.Background()
	usr, err := ResolveUser(ctx, b.userStore, update, b.bootstrapAdminID)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			b.handleUnknownUser(msg)
			return
		}
		b.log.Debug("user resolution failed", "error", err)
		b.reply(msg.Chat.ID, "You don't have access to this bot.")
		return
	}

	if b.hasPendingBroadcast(msg.Chat.ID) {
		b.handleCaptureBroadcast(ctx, msg, usr)
		return
	}

	switch {
	case msg.Text == "/start" || strings.HasPrefix(msg.Text, "/start "):
		b.handleStart(msg, usr)
	case msg.Text == "/unlock" || msg.Text == "🔓 Unlock":
		b.handleUnlock(ctx, msg, usr)
	case msg.Text == "/lock":
		b.handleLock(ctx, msg, usr)
	case msg.Text == "/help":
		b.handleHelp(msg)
	case msg.Text == "⚙️ Admin":
		if err := RequireAdmin(usr); err != nil {
			b.reply(msg.Chat.ID, "Admin access required.")
			return
		}
		b.reply(msg.Chat.ID, "⚙️ Admin Menu", AdminMenuKeyboard())
	default:
		if b.dispatchMessage(ctx, msg, usr) {
			return
		}
		b.reply(msg.Chat.ID, "Unknown command.")
	}
}

func (b *Bot) handleCallbackQuery(update telego.Update) {
	callbackQuery := update.CallbackQuery
	data := callbackQuery.Data

	ctx := context.Background()
	usr, err := ResolveUser(ctx, b.userStore, update, b.bootstrapAdminID)
	if err != nil {
		b.log.Debug("callback user resolution failed", "error", err)
		b.answerCallback(callbackQuery.ID, "Authentication failed.")
		return
	}

	if err := RequireAdmin(usr); err != nil {
		b.answerCallback(callbackQuery.ID, "Admin access required.")
		return
	}

	b.answerCallback(callbackQuery.ID, "")

	if handler, ok := b.callbackHandlers[data]; ok {
		handler(update, usr)
		return
	}

	if strings.HasPrefix(data, "admin_user_") {
		b.handleAdminUserCallback(update, usr, data)
		return
	}

	if strings.HasPrefix(data, "admin_req_") {
		b.handleAdminRequestCallback(update, usr, data)
		return
	}

	b.log.Debug("unknown callback", "data", data)
}

func (b *Bot) dispatchMessage(ctx context.Context, msg *telego.Message, usr *user.User) bool {
	if b.messageHandlers == nil {
		return false
	}
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return false
	}
	handler, ok := b.messageHandlers[parts[0]]
	if !ok {
		return false
	}
	if err := RequireAdmin(usr); err != nil {
		b.reply(msg.Chat.ID, "Admin access required.")
		return true
	}
	handler(ctx, msg, usr, parts[1:])
	return true
}

func (b *Bot) registerMessageHandler(cmd string, handler func(context.Context, *telego.Message, *user.User, []string)) {
	if b.messageHandlers == nil {
		b.messageHandlers = make(map[string]func(context.Context, *telego.Message, *user.User, []string))
	}
	b.messageHandlers[cmd] = handler
}

func (b *Bot) registerCallbackHandler(data string, handler func(telego.Update, *user.User)) {
	if b.callbackHandlers == nil {
		b.callbackHandlers = make(map[string]func(telego.Update, *user.User))
	}
	b.callbackHandlers[data] = handler
}

func (b *Bot) answerCallback(callbackQueryID, text string) {
	err := b.api.AnswerCallbackQuery(context.Background(), &telego.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
		Text:            text,
	})
	if err != nil {
		b.log.Error("answer callback query", "error", err)
	}
}

func (b *Bot) editText(chatID int64, messageID int, text string, markup *telego.InlineKeyboardMarkup) {
	if b.testEdit != nil {
		b.testEdit(chatID, messageID, text, markup)
		return
	}
	params := &telego.EditMessageTextParams{
		ChatID:    telego.ChatID{ID: chatID},
		MessageID: messageID,
		Text:      text,
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	_, err := b.api.EditMessageText(context.Background(), params)
	if err != nil {
		b.log.Error("edit message text", "error", err)
	}
}

func (b *Bot) reply(chatID int64, text string, markup ...telego.ReplyMarkup) {
	if b.testReply != nil {
		b.testReply(chatID, text, markup...)
		return
	}
	params := &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text:   text,
	}
	if len(markup) > 0 {
		params.ReplyMarkup = markup[0]
	}
	_, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		b.log.Error("send message", "error", err)
	}
}

func (b *Bot) handleUnknownUser(msg *telego.Message) {
	tgFrom := msg.From
	if tgFrom == nil {
		return
	}
	if msg.Text == "/start" || strings.HasPrefix(msg.Text, "/start ") {
		b.handleAccessRequest(msg, tgFrom)
		return
	}
	b.reply(msg.Chat.ID, "You don't have access to this bot. Send /start to request access.")
}

func (b *Bot) notifyAdminsOfRequest(req *accessrequest.Request) {
	ctx := context.Background()
	users, err := b.userStore.List(ctx)
	if err != nil {
		b.log.Error("list users for request notification", "error", err)
		return
	}

	name := req.DisplayName
	if req.Username != "" {
		name = fmt.Sprintf("@%s (%s)", req.Username, req.DisplayName)
	}
	text := fmt.Sprintf("🔔 New access request\nFrom: %s\nTelegram ID: %d", name, req.TelegramID)
	kb := &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{{Text: "Review", CallbackData: fmt.Sprintf("admin_req_show_%d", req.TelegramID)}},
		},
	}

	for _, u := range users {
		if u.IsActive && u.Role == RoleAdmin {
			b.reply(u.TelegramID, text, kb)
		}
	}
}
