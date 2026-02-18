package infrastructure

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	reminderdomain "wxbot/internal/contexts/reminder/domain"
)

type JSONReminderRepository struct {
	path string
	mu   sync.Mutex
}

func NewJSONReminderRepository(path string) *JSONReminderRepository {
	return &JSONReminderRepository{path: path}
}

func (r *JSONReminderRepository) SaveAll(ctx context.Context, reminders []reminderdomain.Reminder) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(reminders, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *JSONReminderRepository) LoadAll(ctx context.Context) ([]reminderdomain.Reminder, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	b, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	var out []reminderdomain.Reminder
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}
