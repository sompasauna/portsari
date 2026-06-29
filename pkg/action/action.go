// Package action wires pkg/core packages together into use-case actions.
package action

import (
	"context"
	"log/slog"
	"time"

	"github.com/sompasauna/portsari/pkg/core/accesslog"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/user"
)

// Actions groups all available bot actions.
type Actions struct {
	Unlock    *UnlockAction
	Lock      *LockAction
	SetRole   *SetRoleAction
	SetTier   *SetTierAction
	Remove    *RemoveAction
	Broadcast *BroadcastAction
}

// Config holds configuration for action execution.
type Config struct {
	Timezone            string
	DaytimeStart        string
	DaytimeEnd          string
	UnlockMax           int
	UnlockWindowMinutes int
}

// Result represents the outcome of an action execution.
type Result struct {
	Success bool
	Message string
}

// New creates an Actions group from the given dependencies.
func New(
	userStore *user.Store,
	accessLog *accesslog.Store,
	lockClient *lock.Client,
	cfg Config,
) *Actions {
	log := slog.With("subsystem", "action")
	return &Actions{
		Unlock: &UnlockAction{
			userStore:  userStore,
			accessLog:  accessLog,
			lockClient: lockClient,
			cfg:        cfg,
			log:        log,
		},
		Lock: &LockAction{
			userStore:  userStore,
			accessLog:  accessLog,
			lockClient: lockClient,
			log:        log,
		},
		SetRole: &SetRoleAction{
			userStore: userStore,
			log:       log,
		},
		SetTier: &SetTierAction{
			userStore: userStore,
			log:       log,
		},
		Remove: &RemoveAction{
			userStore: userStore,
			log:       log,
		},
		Broadcast: &BroadcastAction{
			userStore: userStore,
			log:       log,
		},
	}
}

func writeLog(ctx context.Context, log *slog.Logger, store *accesslog.Store, telegramID int64, action, result, detail string) {
	id := telegramID
	err := store.Write(ctx, accesslog.Entry{
		OccurredAt:      time.Now(),
		ActorTelegramID: &id,
		Action:          action,
		Result:          result,
		Detail:          detail,
	})
	if err != nil {
		log.Error("failed to write access log", "action", action, "error", err)
	}
}
