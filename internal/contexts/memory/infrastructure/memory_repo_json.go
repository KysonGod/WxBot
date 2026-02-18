package infrastructure

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	memorydomain "wxbot/internal/contexts/memory/domain"
)

type JSONMemoryRepository struct {
	baseDir string
	mu      sync.Mutex
}

func NewJSONMemoryRepository(baseDir string) *JSONMemoryRepository {
	return &JSONMemoryRepository{baseDir: baseDir}
}

func (r *JSONMemoryRepository) Load(ctx context.Context, userID string) ([]memorydomain.Entry, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	path := filepath.Join(r.baseDir, sanitize(userID)+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	var out []memorydomain.Entry
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *JSONMemoryRepository) Save(ctx context.Context, userID string, entries []memorydomain.Entry) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.baseDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.baseDir, sanitize(userID)+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sanitize(s string) string {
	s = filepath.Base(s)
	s = filepath.Clean(s)
	if s == "." || s == string(filepath.Separator) {
		return "default"
	}
	return s
}
