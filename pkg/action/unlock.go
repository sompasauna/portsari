package action

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/schedule"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// UnlockAction handles door unlock requests.
type UnlockAction struct {
	userStore  *user.Store
	accessLog  *accesslog.Store
	lockClient lockCommander
	cfg        Config
	log        *slog.Logger
}

// Execute attempts to unlock the door for the given user.
func (a *UnlockAction) Execute(ctx context.Context, usr *user.User) Result {
	if !usr.IsActive || (usr.ExpiresAt != nil && time.Now().After(*usr.ExpiresAt)) {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "denied_expired", "denied", "user is not active or expired")
		return Result{Message: "Your access has expired or your account is not active."}
	}

	loc, err := scheduleLocation(a.cfg.Timezone)
	if err != nil {
		a.log.Error("invalid timezone", "timezone", a.cfg.Timezone, "error", err)
	}

	if !schedule.IsUnlockAllowed(usr.Tier, time.Now(), loc, a.cfg.DaytimeStart, a.cfg.DaytimeEnd) {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "denied_tier", "denied", "tier not allowed at this time")
		return Result{Message: "Your tier does not allow unlocking at this time."}
	}

	count, err := a.accessLog.RecentSuccessCount(ctx, usr.TelegramID, a.cfg.UnlockWindowMinutes)
	if err != nil {
		a.log.Error("rate limit check failed", "error", err)
	} else if count >= a.cfg.UnlockMax {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "denied_rate_limit", "denied", "rate limit exceeded")
		return Result{Message: fmt.Sprintf("Rate limit reached (%d unlocks in %d minutes).", a.cfg.UnlockMax, a.cfg.UnlockWindowMinutes)}
	}

	confirmed, err := a.lockClient.Unlock(ctx)
	if err != nil {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "unlock", "error", err.Error())
		return Result{Message: "An error occurred while unlocking the door."}
	}
	if !confirmed {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "unlock", "timeout", "confirmation timeout")
		return Result{Message: "Unlock command sent but confirmation timed out."}
	}

	writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "unlock", "success", "")
	return Result{Success: true, Message: "Door unlocked."}
}

func scheduleLocation(timezone string) (*time.Location, error) {
	if timezone == "" {
		return hostLocalLocation()
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		fallback, fallbackErr := hostLocalLocation()
		if fallbackErr != nil {
			return time.UTC, err
		}
		return fallback, err
	}
	return loc, nil
}

func hostLocalLocation() (*time.Location, error) {
	return time.LoadLocation("Local")
}
