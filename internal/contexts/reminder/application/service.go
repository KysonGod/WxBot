package application

import (
	"context"

	reminderdomain "wxbot/internal/contexts/reminder/domain"
)

type Repository interface {
	SaveAll(ctx context.Context, reminders []reminderdomain.Reminder) error
	LoadAll(ctx context.Context) ([]reminderdomain.Reminder, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Start(ctx context.Context) {
	_ = ctx
	// 占位：后续可在此挂载短期/长期提醒调度器。
}
