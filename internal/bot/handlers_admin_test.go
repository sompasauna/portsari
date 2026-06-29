package bot

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/sompasauna/portsari/pkg/action"
	"github.com/sompasauna/portsari/pkg/core/lock"
	"github.com/sompasauna/portsari/pkg/core/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingBroadcastState(t *testing.T) {
	bot := &Bot{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	assert.False(t, bot.hasPendingBroadcast(123))

	bot.setPendingBroadcast(123)
	assert.True(t, bot.hasPendingBroadcast(123))
	assert.False(t, bot.hasPendingBroadcast(456))

	bot.clearPendingBroadcast(123)
	assert.False(t, bot.hasPendingBroadcast(123))
}

func TestHandleCaptureBroadcast(t *testing.T) {
	db := newTestDB(t)
	userStore := user.New(db)
	ctx := context.Background()

	_, err := userStore.Create(ctx, 100, "active1", "Active 1", "user", "full", nil)
	require.NoError(t, err)
	_, err = userStore.Create(ctx, 200, "active2", "Active 2", "user", "full", nil)
	require.NoError(t, err)

	actions := action.New(userStore, nil, (*lock.Client)(nil), action.Config{})

	var sentMessages []string
	var repliedText string

	actions.Broadcast.SendMessage = func(chatID int64, text string) {
		sentMessages = append(sentMessages, text)
	}

	bot := &Bot{
		actions: actions,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		testReply: func(chatID int64, text string, markup ...telego.ReplyMarkup) {
			repliedText = text
		},
	}

	msg := &telego.Message{
		Chat: telego.Chat{ID: 123},
		Text: "Test broadcast",
	}
	usr := &user.User{TelegramID: 789, Role: "admin"}

	bot.handleCaptureBroadcast(ctx, msg, usr)

	require.Len(t, sentMessages, 2)
	assert.Contains(t, sentMessages[0], "Test broadcast")
	assert.Contains(t, repliedText, "Broadcast sent")
}

func TestHandleCaptureBroadcast_Cancel(t *testing.T) {
	bot := &Bot{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		testReply: func(chatID int64, text string, markup ...telego.ReplyMarkup) {
			assert.Contains(t, text, "cancelled")
		},
	}

	msg := &telego.Message{Chat: telego.Chat{ID: 123}, Text: "/cancel"}
	usr := &user.User{TelegramID: 789, Role: "admin"}

	bot.handleCaptureBroadcast(context.Background(), msg, usr)
}

func TestHandleCaptureBroadcast_StoreError(t *testing.T) {
	actions := action.New(user.New(newTestDB(t)), nil, (*lock.Client)(nil), action.Config{})

	var repliedText string
	bot := &Bot{
		actions: actions,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		testReply: func(chatID int64, text string, markup ...telego.ReplyMarkup) {
			repliedText = text
		},
	}

	msg := &telego.Message{Chat: telego.Chat{ID: 123}, Text: ""}
	usr := &user.User{TelegramID: 789, Role: "admin"}

	bot.handleCaptureBroadcast(context.Background(), msg, usr)

	assert.Contains(t, repliedText, "Error")
}

func TestHandleCaptureBroadcast_NonAdmin(t *testing.T) {
	bot := &Bot{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		testReply: func(chatID int64, text string, markup ...telego.ReplyMarkup) {
			assert.Contains(t, text, "Admin access required")
		},
	}

	msg := &telego.Message{Chat: telego.Chat{ID: 123}, Text: "some message"}
	usr := &user.User{TelegramID: 789, Role: "user"}

	bot.handleCaptureBroadcast(context.Background(), msg, usr)
}
