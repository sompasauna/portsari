package action

import (
	"context"
	"log/slog"
	"time"

	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// LockAction handles door lock requests.
type LockAction struct {
	userStore  *user.Store
	accessLog  *accesslog.Store
	lockClient lockCommander
	log        *slog.Logger
}

// Execute attempts to lock the door for the given user.
func (a *LockAction) Execute(ctx context.Context, usr *user.User) Result {
	if !usr.IsActive || (usr.ExpiresAt != nil && time.Now().After(*usr.ExpiresAt)) {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "denied_expired", "denied", "user is not active or expired")
		return Result{Message: "Your access has expired or your account is not active."}
	}

	confirmed, err := a.lockClient.Lock(ctx)
	if err != nil {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "lock", "error", err.Error())
		return Result{Message: "An error occurred while locking the door."}
	}
	if !confirmed {
		writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "lock", "timeout", "confirmation timeout")
		return Result{Message: "Lock command sent but confirmation timed out."}
	}

	writeLog(ctx, a.log, a.accessLog, usr.TelegramID, "lock", "success", "")
	return Result{Success: true, Message: "Door locked."}
}
