package action

import "context"

type lockCommander interface {
	Lock(ctx context.Context) (bool, error)
	Unlock(ctx context.Context) (bool, error)
}
