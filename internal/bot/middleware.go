package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// RoleAdmin is the admin role string used in comparisons.
const RoleAdmin = "admin"

// statusPending is the access-request status for requests awaiting admin review.
const statusPending = "pending"

// ResolveUser resolves the Telegram user from an update, creating them if needed.
// If bootstrapAdminID is set and no admin exists yet, creates as admin.
func ResolveUser(ctx context.Context, userStore *user.Store, update telego.Update, bootstrapAdminID int64) (*user.User, error) {
	tgUser := extractTelegramUser(update)
	if tgUser == nil {
		return nil, errors.New("bot: no user in update")
	}

	usr, err := userStore.Get(ctx, tgUser.ID)
	if err != nil {
		if !errors.Is(err, user.ErrNotFound) {
			return nil, fmt.Errorf("bot: get user: %w", err)
		}
		return tryCreateBootstrapAdmin(ctx, userStore, tgUser, bootstrapAdminID)
	}

	if !usr.IsActive {
		return nil, errors.New("bot: user is inactive")
	}
	if usr.ExpiresAt != nil && time.Now().After(*usr.ExpiresAt) {
		return nil, errors.New("bot: user is expired")
	}

	if err := userStore.SetLastSeen(ctx, tgUser.ID, time.Now().UTC()); err != nil {
		slog.With("subsystem", "telegram").Warn("update last_seen", "error", err)
	}

	return usr, nil
}

// RequireAdmin checks that the user has the admin role.
func RequireAdmin(usr *user.User) error {
	if usr.Role != RoleAdmin {
		return errors.New("bot: admin role required")
	}
	return nil
}

func extractTelegramUser(update telego.Update) *telego.User {
	if update.Message != nil {
		return update.Message.From
	}
	if update.CallbackQuery != nil {
		from := update.CallbackQuery.From
		return &from
	}
	return nil
}

// tryCreateBootstrapAdmin creates the first admin from env config when no admins exist yet.
// Returns user.ErrNotFound if the conditions aren't met, so callers can treat it as "not found".
func tryCreateBootstrapAdmin(ctx context.Context, userStore *user.Store, tgUser *telego.User, bootstrapAdminID int64) (*user.User, error) {
	if bootstrapAdminID <= 0 || tgUser.ID != bootstrapAdminID {
		return nil, user.ErrNotFound
	}
	count, err := userStore.CountAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("bot: count admins: %w", err)
	}
	if count > 0 {
		return nil, user.ErrNotFound
	}
	return createUser(ctx, userStore, tgUser, bootstrapAdminID)
}

func createUser(ctx context.Context, userStore *user.Store, tgUser *telego.User, bootstrapAdminID int64) (*user.User, error) {
	displayName := tgUser.FirstName
	if tgUser.LastName != "" {
		displayName = tgUser.FirstName + " " + tgUser.LastName
	}

	role := "user"
	if bootstrapAdminID > 0 && tgUser.ID == bootstrapAdminID {
		count, err := userStore.CountAdmins(ctx)
		if err != nil {
			return nil, fmt.Errorf("bot: count admins: %w", err)
		}
		if count == 0 {
			role = RoleAdmin
		}
	}

	usr, err := userStore.Create(ctx, tgUser.ID, tgUser.Username, displayName, role, "full", nil)
	if err != nil {
		return nil, fmt.Errorf("bot: create user: %w", err)
	}
	return usr, nil
}
