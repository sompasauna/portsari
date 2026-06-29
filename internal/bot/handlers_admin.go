package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/action"
	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/accessrequest"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// RegisterAdminHandlers registers admin-only handlers on the bot.
// Must be called after RegisterUserHandlers (or at least after the bot has actions).
func RegisterAdminHandlers(bot *Bot, actions *action.Actions, accessLog *accesslog.Store, userStore *user.Store, lockClient *lock.Client) {
	if bot.actions == nil {
		bot.RegisterActions(actions)
	}
	bot.accessLog = accessLog
	bot.lockClient = lockClient

	slashHandlers := map[string]func(context.Context, *telego.Message, *user.User, []string){
		"/status":    bot.handleAdminStatus,
		"/users":     bot.handleAdminUsers,
		"/log":       bot.handleAdminLog,
		"/broadcast": bot.handleAdminBroadcast,
		"/setrole":   bot.handleAdminSetRole,
		"/settier":   bot.handleAdminSetTier,
		"/remove":    bot.handleAdminRemove,
	}
	for cmd, handler := range slashHandlers {
		bot.registerMessageHandler(cmd, handler)
	}

	callbackHandlers := map[string]func(telego.Update, *user.User){
		"admin_menu":       bot.handleAdminMenuCallback,
		"admin_status":     bot.handleAdminStatusCallback,
		"admin_users":      func(upd telego.Update, usr *user.User) { bot.handleAdminUsersCallback(upd, usr, userStore) },
		"admin_log":        func(upd telego.Update, usr *user.User) { bot.handleAdminLogCallback(upd, usr, accessLog) },
		"admin_broadcast":  bot.handleAdminBroadcastCallback,
		"admin_back":       bot.handleAdminMenuCallback,
		"admin_back_users": func(upd telego.Update, usr *user.User) { bot.handleAdminUsersCallback(upd, usr, userStore) },
		"admin_requests":   bot.handleAdminRequestsCallback,
	}
	for data, handler := range callbackHandlers {
		bot.registerCallbackHandler(data, handler)
	}
}

// ---------------------------------------------------------------------------
// Inline callback handlers (edit the original message).
// ---------------------------------------------------------------------------

func (b *Bot) handleAdminMenuCallback(upd telego.Update, _ *user.User) {
	cq := upd.CallbackQuery
	b.editText(cq.Message.GetChat().ID, cq.Message.GetMessageID(), "⚙️ Admin Menu", AdminMenuKeyboard())
}

func (b *Bot) handleAdminStatusCallback(upd telego.Update, _ *user.User) {
	state := b.lockClient.State()
	text := formatLockStatus(state)
	b.editText(upd.CallbackQuery.Message.GetChat().ID, upd.CallbackQuery.Message.GetMessageID(),
		text, AdminBackKeyboard("admin_menu"))
}

func (b *Bot) handleAdminUsersCallback(upd telego.Update, _ *user.User, userStore *user.Store) {
	ctx := context.Background()
	users, err := userStore.List(ctx)
	if err != nil {
		b.log.Error("list users for callback", "error", err)
		b.editText(upd.CallbackQuery.Message.GetChat().ID, upd.CallbackQuery.Message.GetMessageID(), "Error fetching users.", AdminBackKeyboard("admin_menu"))
		return
	}
	text := formatUsersList(users)
	b.editText(upd.CallbackQuery.Message.GetChat().ID, upd.CallbackQuery.Message.GetMessageID(), text, AdminUserListKeyboard(users))
}

func (b *Bot) handleAdminLogCallback(upd telego.Update, _ *user.User, accessLog *accesslog.Store) {
	ctx := context.Background()
	entries, err := accessLog.List(ctx, 20)
	if err != nil {
		b.log.Error("list log for callback", "error", err)
		b.editText(upd.CallbackQuery.Message.GetChat().ID, upd.CallbackQuery.Message.GetMessageID(), "Error fetching log.", AdminBackKeyboard("admin_menu"))
		return
	}
	text := formatLogEntries(entries)
	b.editText(upd.CallbackQuery.Message.GetChat().ID, upd.CallbackQuery.Message.GetMessageID(), text, AdminBackKeyboard("admin_menu"))
}

func (b *Bot) handleAdminBroadcastCallback(upd telego.Update, _ *user.User) {
	chatID := upd.CallbackQuery.Message.GetChat().ID
	messageID := upd.CallbackQuery.Message.GetMessageID()

	b.setPendingBroadcast(chatID)

	b.editText(chatID, messageID,
		"📢 Type your broadcast message below as a new message.\nOr type /cancel to cancel.",
		AdminBackKeyboard("admin_menu"))
}

// handleCaptureBroadcast handles a captured message from the inline broadcast flow.
func (b *Bot) handleCaptureBroadcast(ctx context.Context, msg *telego.Message, usr *user.User) {
	b.clearPendingBroadcast(msg.Chat.ID)

	if err := RequireAdmin(usr); err != nil {
		b.reply(msg.Chat.ID, "Admin access required.")
		return
	}

	text := msg.Text
	if text == "/cancel" {
		b.reply(msg.Chat.ID, "Broadcast cancelled.", AdminKeyboard())
		return
	}

	if err := b.actions.Broadcast.Execute(ctx, text); err != nil {
		b.reply(msg.Chat.ID, fmt.Sprintf("Error: %v", err), AdminKeyboard())
		return
	}
	b.reply(msg.Chat.ID, "📢 Broadcast sent to all active users.", AdminKeyboard())
}

// handleAdminUserCallback handles admin_user_<id> and admin_user_<id>_<action> callbacks.
func (b *Bot) handleAdminUserCallback(upd telego.Update, _ *user.User, data string) {
	cq := upd.CallbackQuery
	chatID := cq.Message.GetChat().ID
	msgID := cq.Message.GetMessageID()
	ctx := context.Background()

	parts := strings.Split(data, "_")
	// parts: ["admin", "user", "<id>", ...]
	if len(parts) < 3 {
		return
	}
	telegramID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		b.log.Error("invalid user id in callback", "data", data, "error", err)
		return
	}

	if len(parts) == 3 {
		// Show user detail and actions.
		target, err := b.userStore.Get(ctx, telegramID)
		if err != nil {
			b.editText(chatID, msgID, "User not found.", AdminBackKeyboard("admin_users"))
			return
		}
		b.editText(chatID, msgID, formatUserDetail(*target), AdminUserActionsKeyboard(*target))
		return
	}

	// Action: role_admin, role_user, tier_full, tier_daytime, remove.
	action := strings.Join(parts[3:], "_")
	switch action {
	case "role_admin", "role_user":
		role := strings.TrimPrefix(action, "role_")
		if err := b.actions.SetRole.Execute(ctx, telegramID, role); err != nil {
			b.editText(chatID, msgID, fmt.Sprintf("Error: %v", err), AdminBackKeyboard("admin_users"))
			return
		}
		b.editText(chatID, msgID, fmt.Sprintf("Role updated to %s.", role), AdminBackKeyboard("admin_users"))
	case "tier_full", "tier_daytime":
		tier := strings.TrimPrefix(action, "tier_")
		if err := b.actions.SetTier.Execute(ctx, telegramID, tier); err != nil {
			b.editText(chatID, msgID, fmt.Sprintf("Error: %v", err), AdminBackKeyboard("admin_users"))
			return
		}
		b.editText(chatID, msgID, fmt.Sprintf("Tier updated to %s.", tier), AdminBackKeyboard("admin_users"))
	case "remove":
		if err := b.actions.Remove.Execute(ctx, telegramID); err != nil {
			b.editText(chatID, msgID, fmt.Sprintf("Error: %v", err), AdminBackKeyboard("admin_users"))
			return
		}
		b.editText(chatID, msgID, "User removed (deactivated).", AdminBackKeyboard("admin_users"))
	}
}

// ---------------------------------------------------------------------------
// Slash command handlers (reply with new messages).
// ---------------------------------------------------------------------------

func (b *Bot) handleAdminStatus(_ context.Context, msg *telego.Message, _ *user.User, _ []string) {
	state := b.lockClient.State()
	text := formatLockStatus(state)
	b.reply(msg.Chat.ID, text, AdminKeyboard())
}

func (b *Bot) handleAdminUsers(ctx context.Context, msg *telego.Message, _ *user.User, _ []string) {
	users, err := b.userStore.List(ctx)
	if err != nil {
		b.log.Error("list users", "error", err)
		b.reply(msg.Chat.ID, "Error fetching users.", AdminKeyboard())
		return
	}
	text := formatUsersList(users)
	b.reply(msg.Chat.ID, text, AdminKeyboard())
}

func (b *Bot) handleAdminLog(ctx context.Context, msg *telego.Message, _ *user.User, args []string) {
	n := 20
	if len(args) >= 1 {
		parsed, err := strconv.Atoi(args[0])
		if err == nil && parsed > 0 && parsed <= 100 {
			n = parsed
		}
	}

	entries, err := b.accessLog.List(ctx, n)
	if err != nil {
		b.log.Error("list log", "error", err)
		b.reply(msg.Chat.ID, "Error fetching log entries.", AdminKeyboard())
		return
	}
	text := formatLogEntries(entries)
	b.reply(msg.Chat.ID, text, AdminKeyboard())
}

func (b *Bot) handleAdminBroadcast(ctx context.Context, msg *telego.Message, _ *user.User, args []string) {
	if len(args) < 1 {
		b.reply(msg.Chat.ID, "Usage: /broadcast <message>", AdminKeyboard())
		return
	}

	message := strings.Join(args, " ")
	if err := b.actions.Broadcast.Execute(ctx, message); err != nil {
		b.reply(msg.Chat.ID, fmt.Sprintf("Error: %v", err), AdminKeyboard())
		return
	}

	b.reply(msg.Chat.ID, "📢 Broadcast sent to all active users.", AdminKeyboard())
}

func (b *Bot) handleAdminSetRole(ctx context.Context, msg *telego.Message, _ *user.User, args []string) {
	if len(args) < 2 {
		b.reply(msg.Chat.ID, "Usage: /setrole <telegram_id> <role>", AdminKeyboard())
		return
	}
	telegramID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		b.reply(msg.Chat.ID, "Invalid telegram_id.", AdminKeyboard())
		return
	}
	role := args[1]
	if err := b.actions.SetRole.Execute(ctx, telegramID, role); err != nil {
		b.reply(msg.Chat.ID, fmt.Sprintf("Error: %v", err), AdminKeyboard())
		return
	}
	b.reply(msg.Chat.ID, fmt.Sprintf("User %d role set to %s.", telegramID, role), AdminKeyboard())
}

func (b *Bot) handleAdminSetTier(ctx context.Context, msg *telego.Message, _ *user.User, args []string) {
	if len(args) < 2 {
		b.reply(msg.Chat.ID, "Usage: /settier <telegram_id> <tier>", AdminKeyboard())
		return
	}
	telegramID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		b.reply(msg.Chat.ID, "Invalid telegram_id.", AdminKeyboard())
		return
	}
	tier := args[1]
	if err := b.actions.SetTier.Execute(ctx, telegramID, tier); err != nil {
		b.reply(msg.Chat.ID, fmt.Sprintf("Error: %v", err), AdminKeyboard())
		return
	}
	b.reply(msg.Chat.ID, fmt.Sprintf("User %d tier set to %s.", telegramID, tier), AdminKeyboard())
}

func (b *Bot) handleAdminRemove(ctx context.Context, msg *telego.Message, _ *user.User, args []string) {
	if len(args) < 1 {
		b.reply(msg.Chat.ID, "Usage: /remove <telegram_id>", AdminKeyboard())
		return
	}
	telegramID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		b.reply(msg.Chat.ID, "Invalid telegram_id.", AdminKeyboard())
		return
	}
	if err := b.actions.Remove.Execute(ctx, telegramID); err != nil {
		b.reply(msg.Chat.ID, fmt.Sprintf("Error: %v", err), AdminKeyboard())
		return
	}
	b.reply(msg.Chat.ID, fmt.Sprintf("User %d removed (deactivated).", telegramID), AdminKeyboard())
}

// ---------------------------------------------------------------------------
// Formatting helpers.
// ---------------------------------------------------------------------------

func formatUsersList(users []user.User) string {
	if len(users) == 0 {
		return "No users found."
	}
	var buf strings.Builder
	buf.WriteString("Users:\n")
	for i, u := range users {
		fmt.Fprintf(&buf, "%d. %s\n", i+1, formatUserLine(u))
	}
	return buf.String()
}

func formatLogEntries(entries []accesslog.Entry) string {
	if len(entries) == 0 {
		return "No log entries."
	}
	var b strings.Builder
	b.WriteString("Recent log:\n")
	for _, e := range entries {
		b.WriteString(formatLogEntry(e))
		b.WriteString("\n")
	}
	return b.String()
}

func formatLogEntry(e accesslog.Entry) string {
	timestamp := e.OccurredAt.Format("2006-01-02 15:04")
	actor := "external"
	if e.ActorTelegramID != nil {
		actor = fmt.Sprintf("@%d", *e.ActorTelegramID)
	}
	return fmt.Sprintf("%s - %s by %s - %s", timestamp, e.Action, actor, e.Result)
}

// ---------------------------------------------------------------------------
// Access request callbacks.
// ---------------------------------------------------------------------------

func (b *Bot) handleAdminRequestsCallback(upd telego.Update, _ *user.User) {
	chatID := upd.CallbackQuery.Message.GetChat().ID
	msgID := upd.CallbackQuery.Message.GetMessageID()
	ctx := context.Background()

	requests, err := b.accessRequestStore.ListPending(ctx)
	if err != nil {
		b.editText(chatID, msgID, "Error fetching requests.", AdminBackKeyboard("admin_menu"))
		return
	}
	if len(requests) == 0 {
		b.editText(chatID, msgID, "No pending access requests.", AdminBackKeyboard("admin_menu"))
		return
	}
	text := fmt.Sprintf("Pending access requests (%d):", len(requests))
	b.editText(chatID, msgID, text, AdminRequestListKeyboard(requests))
}

// handleAdminRequestCallback handles the admin_req_* callback chain.
// Callback data format:
//
//	admin_req_show_<id>
//	admin_req_approve_<id>
//	admin_req_role_<id>_<role>
//	admin_req_tier_<id>_<role>_<tier>
//	admin_req_grant_<id>_<role>_<tier>_<expiry>   expiry: perm|7d|30d
//	admin_req_reject_<id>
func (b *Bot) handleAdminRequestCallback(upd telego.Update, _ *user.User, data string) {
	cq := upd.CallbackQuery
	chatID := cq.Message.GetChat().ID
	msgID := cq.Message.GetMessageID()
	ctx := context.Background()

	parts := strings.Split(data, "_")
	// parts: ["admin", "req", <action>, <telegramID>, ...]
	if len(parts) < 4 {
		return
	}
	action := parts[2]
	telegramID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		b.log.Error("invalid telegram id in request callback", "data", data)
		return
	}

	switch action {
	case "show":
		req, err := b.accessRequestStore.GetLatestByTelegramID(ctx, telegramID)
		if err != nil {
			b.editText(chatID, msgID, "Request not found.", AdminBackKeyboard("admin_requests"))
			return
		}
		b.editText(chatID, msgID, formatRequestDetail(*req), AdminRequestDetailKeyboard(telegramID))

	case "approve":
		b.editText(chatID, msgID, "Select role:", AdminRequestRoleKeyboard(telegramID))

	case "role":
		if len(parts) < 5 {
			return
		}
		role := parts[4]
		b.editText(chatID, msgID, "Select tier:", AdminRequestTierKeyboard(telegramID, role))

	case "tier":
		if len(parts) < 6 {
			return
		}
		role, tier := parts[4], parts[5]
		b.editText(chatID, msgID, "Select access duration:", AdminRequestExpiryKeyboard(telegramID, role, tier))

	case "grant":
		b.handleAdminRequestGrant(ctx, chatID, msgID, telegramID, parts)

	case "reject":
		req, err := b.accessRequestStore.GetLatestByTelegramID(ctx, telegramID)
		if err != nil {
			b.editText(chatID, msgID, "Request not found.", AdminBackKeyboard("admin_requests"))
			return
		}
		if req.Status != statusPending {
			b.editText(chatID, msgID, "This request has already been handled.", AdminBackKeyboard("admin_requests"))
			return
		}
		if err := b.accessRequestStore.Reject(ctx, telegramID); err != nil && !errors.Is(err, accessrequest.ErrNotFound) {
			b.log.Error("reject request", "error", err)
			b.editText(chatID, msgID, fmt.Sprintf("Error: %v", err), AdminBackKeyboard("admin_requests"))
			return
		}

		b.reply(telegramID, "Your access request was not approved.")

		name := req.DisplayName
		if req.Username != "" {
			name = "@" + req.Username
		}
		b.editText(chatID, msgID, fmt.Sprintf("❌ Rejected %s's request.", name), AdminBackKeyboard("admin_requests"))
	}
}

func (b *Bot) handleAdminRequestGrant(ctx context.Context, chatID int64, msgID int, telegramID int64, parts []string) {
	if len(parts) < 7 {
		return
	}
	role, tier, expiry := parts[4], parts[5], parts[6]

	req, err := b.accessRequestStore.GetLatestByTelegramID(ctx, telegramID)
	if err != nil || req.Status != statusPending {
		b.editText(chatID, msgID, "This request has already been handled.", AdminBackKeyboard("admin_requests"))
		return
	}

	var expiresAt *time.Time
	switch expiry {
	case "7d":
		t := time.Now().Add(7 * 24 * time.Hour)
		expiresAt = &t
	case "30d":
		t := time.Now().Add(30 * 24 * time.Hour)
		expiresAt = &t
	}

	if _, err := b.userStore.Create(ctx, telegramID, req.Username, req.DisplayName, role, tier, expiresAt); err != nil {
		b.log.Error("create user from request", "error", err)
		b.editText(chatID, msgID, fmt.Sprintf("Error creating user: %v", err), AdminBackKeyboard("admin_requests"))
		return
	}
	if err := b.accessRequestStore.Approve(ctx, telegramID); err != nil {
		b.log.Warn("mark request approved", "error", err)
	}

	welcomeText := fmt.Sprintf("✅ Your access request has been approved!\nRole: %s, Tier: %s", role, tier)
	if expiresAt != nil {
		welcomeText += "\nExpires: " + expiresAt.Format("2006-01-02")
	}
	var kb telego.ReplyMarkup = MainKeyboard()
	if role == RoleAdmin {
		kb = AdminKeyboard()
	}
	b.reply(telegramID, welcomeText, kb)

	name := req.DisplayName
	if req.Username != "" {
		name = "@" + req.Username
	}
	b.editText(chatID, msgID, fmt.Sprintf("✅ Approved %s as %s/%s.", name, role, tier), AdminBackKeyboard("admin_requests"))
}

func formatRequestDetail(req accessrequest.Request) string {
	name := req.DisplayName
	if req.Username != "" {
		name = fmt.Sprintf("@%s (%s)", req.Username, req.DisplayName)
	}
	return fmt.Sprintf("📬 Access Request\nFrom: %s\nTelegram ID: %d\nRequested: %s",
		name, req.TelegramID, req.RequestedAt.Format("2006-01-02 15:04"))
}

func formatLockStatus(state lock.LockState) string {
	stateEmoji := "❓"
	switch state.Lock {
	case lock.StateLocked:
		stateEmoji = "🔒"
	case lock.StateUnlocked:
		stateEmoji = "🔓"
	case lock.StateJammed:
		stateEmoji = "⚠️"
	}

	stateText := string(state.Lock)
	if !state.Available {
		stateText = "unavailable"
	}

	batteryText := "N/A"
	if state.Battery > 0 {
		batteryText = fmt.Sprintf("%d%%", state.Battery)
	}

	return fmt.Sprintf("%s State: %s\n🔋 Battery: %s", stateEmoji, stateText, batteryText)
}
