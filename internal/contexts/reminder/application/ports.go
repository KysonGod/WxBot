package application

import (
	"context"

	reminderdomain "wxbot/internal/contexts/reminder/domain"
)

type ReminderQueue interface {
	EnqueueReminder(ctx context.Context, userID, content string) error
}

type Parser interface {
	Parse(ctx context.Context, userID, content string) (*reminderdomain.Reminder, error)
}
