package bot

import (
	"fmt"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/core/accessrequest"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// MainKeyboard returns the persistent reply keyboard for a regular user.
func MainKeyboard() *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			{telego.KeyboardButton{Text: "\U0001f513 Unlock"}},
		},
		ResizeKeyboard: true,
	}
}

// AdminKeyboard returns the persistent reply keyboard for an admin user.
func AdminKeyboard() *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			{telego.KeyboardButton{Text: "\U0001f513 Unlock"}},
			{telego.KeyboardButton{Text: "\u2699\ufe0f Admin"}},
		},
		ResizeKeyboard: true,
	}
}

// AdminMenuKeyboard returns the inline keyboard for the admin submenu.
func AdminMenuKeyboard() *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{Text: "📋 Status", CallbackData: "admin_status"},
				{Text: "👥 Users", CallbackData: "admin_users"},
			},
			{
				{Text: "✉️ Invite", CallbackData: "admin_invite"},
				{Text: "📜 Full log", CallbackData: "admin_log"},
			},
			{
				{Text: "📢 Broadcast", CallbackData: "admin_broadcast"},
				{Text: "📬 Requests", CallbackData: "admin_requests"},
			},
		},
	}
}

// AdminBackKeyboard returns a single-button inline keyboard with a Back button.
func AdminBackKeyboard(callbackData string) *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{{Text: "← Back", CallbackData: callbackData}},
		},
	}
}

// AdminUserListKeyboard returns an inline keyboard with one button per active user.
func AdminUserListKeyboard(users []user.User) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, len(users)+1)
	for _, u := range users {
		label := formatUserButtonLabel(u)
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: label, CallbackData: fmt.Sprintf("admin_user_%d", u.TelegramID)},
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		{Text: "← Back", CallbackData: "admin_back"},
	})
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

// AdminUserActionsKeyboard returns an inline keyboard for managing a specific user.
func AdminUserActionsKeyboard(u user.User) *telego.InlineKeyboardMarkup {
	uid := u.TelegramID
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{Text: "Set Admin", CallbackData: fmt.Sprintf("admin_user_%d_role_admin", uid)},
				{Text: "Set User", CallbackData: fmt.Sprintf("admin_user_%d_role_user", uid)},
			},
			{
				{Text: "Set Full", CallbackData: fmt.Sprintf("admin_user_%d_tier_full", uid)},
				{Text: "Set Daytime", CallbackData: fmt.Sprintf("admin_user_%d_tier_daytime", uid)},
			},
			{
				{Text: "Remove", CallbackData: fmt.Sprintf("admin_user_%d_remove", uid)},
			},
			{
				{Text: "← Back to users", CallbackData: "admin_back_users"},
			},
		},
	}
}

// AdminRequestListKeyboard returns a keyboard listing pending access requests.
func AdminRequestListKeyboard(requests []accessrequest.Request) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, len(requests)+1)
	for _, r := range requests {
		label := r.DisplayName
		if r.Username != "" {
			label = "@" + r.Username + " (" + r.DisplayName + ")"
		}
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: "👤 " + label, CallbackData: fmt.Sprintf("admin_req_show_%d", r.TelegramID)},
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		{Text: "← Back", CallbackData: "admin_menu"},
	})
	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// AdminRequestDetailKeyboard returns the approve/reject keyboard for a single request.
func AdminRequestDetailKeyboard(telegramID int64) *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{Text: "✅ Approve", CallbackData: fmt.Sprintf("admin_req_approve_%d", telegramID)},
				{Text: "❌ Reject", CallbackData: fmt.Sprintf("admin_req_reject_%d", telegramID)},
			},
			{{Text: "← Back", CallbackData: "admin_requests"}},
		},
	}
}

// AdminRequestRoleKeyboard returns a role picker for approving a request.
func AdminRequestRoleKeyboard(telegramID int64) *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{Text: "User", CallbackData: fmt.Sprintf("admin_req_role_%d_user", telegramID)},
				{Text: "Admin", CallbackData: fmt.Sprintf("admin_req_role_%d_admin", telegramID)},
			},
			{{Text: "← Back", CallbackData: fmt.Sprintf("admin_req_show_%d", telegramID)}},
		},
	}
}

// AdminRequestTierKeyboard returns a tier picker for approving a request.
func AdminRequestTierKeyboard(telegramID int64, role string) *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{Text: "Full", CallbackData: fmt.Sprintf("admin_req_tier_%d_%s_full", telegramID, role)},
				{Text: "Daytime", CallbackData: fmt.Sprintf("admin_req_tier_%d_%s_daytime", telegramID, role)},
			},
			{{Text: "← Back", CallbackData: fmt.Sprintf("admin_req_approve_%d", telegramID)}},
		},
	}
}

// AdminRequestExpiryKeyboard returns an expiry picker for approving a request.
func AdminRequestExpiryKeyboard(telegramID int64, role, tier string) *telego.InlineKeyboardMarkup {
	prefix := fmt.Sprintf("admin_req_grant_%d_%s_%s", telegramID, role, tier)
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{Text: "Permanent", CallbackData: prefix + "_perm"},
				{Text: "7 days", CallbackData: prefix + "_7d"},
				{Text: "30 days", CallbackData: prefix + "_30d"},
			},
			{{Text: "← Back", CallbackData: fmt.Sprintf("admin_req_role_%d_%s", telegramID, role)}},
		},
	}
}

func formatUserButtonLabel(user user.User) string {
	if user.Username != "" {
		return fmt.Sprintf("👤 @%s (%s, %s)", user.Username, user.Role, user.Tier)
	}
	return fmt.Sprintf("👤 %s (%s, %s)", user.DisplayName, user.Role, user.Tier)
}

func formatUserLine(user user.User) string {
	name := user.DisplayName
	if user.Username != "" {
		name = "@" + user.Username
	}
	status := "✅ Active"
	if !user.IsActive {
		status = "❌ Inactive"
	}
	expiry := ""
	if user.ExpiresAt != nil {
		expiry = " ⏳ Expires " + user.ExpiresAt.Format("2006-01-02")
	}
	return fmt.Sprintf("%s (%s, %s) %s%s", name, user.Role, user.Tier, status, expiry)
}

func formatUserDetail(user user.User) string {
	name := user.DisplayName
	if user.Username != "" {
		name = "@" + user.Username
	}
	status := "✅ Active"
	if !user.IsActive {
		status = "❌ Inactive"
	}
	expiry := "None"
	if user.ExpiresAt != nil {
		expiry = user.ExpiresAt.Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("👤 User: %s\nID: %d\nRole: %s | Tier: %s\nActive: %s\nExpires: %s", name, user.TelegramID, user.Role, user.Tier, status, expiry)
}
