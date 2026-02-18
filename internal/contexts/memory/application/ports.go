package application

import (
	"context"

	memorydomain "wxbot/internal/contexts/memory/domain"
)

type Repository interface {
	Load(ctx context.Context, userID string) ([]memorydomain.Entry, error)
	Save(ctx context.Context, userID string, entries []memorydomain.Entry) error
}

type TempLogRepository interface {
	AppendUser(ctx context.Context, userID, text string) error
	AppendAssistant(ctx context.Context, userID, text string) error
}
