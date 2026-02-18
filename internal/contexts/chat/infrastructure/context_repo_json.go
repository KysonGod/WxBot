package infrastructure

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	chatdomain "wxbot/internal/contexts/chat/domain"
)

type storedTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	At      string `json:"at"`
}

type JSONContextRepository struct {
	path string
	mu   sync.Mutex
	data map[string][]storedTurn
}

func NewJSONContextRepository(path string) (*JSONContextRepository, error) {
	r := &JSONContextRepository{path: path, data: map[string][]storedTurn{}}
	if err := r.loadFromDisk(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *JSONContextRepository) LoadConversation(ctx context.Context, userID string, maxTurns int) (chatdomain.Conversation, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	rows := r.data[userID]
	turns := make([]chatdomain.Turn, 0, len(rows))
	for _, row := range rows {
		at := time.Now()
		if row.At != "" {
			if parsed, err := time.Parse(time.RFC3339, row.At); err == nil {
				at = parsed
			}
		}
		turns = append(turns, chatdomain.Turn{Role: row.Role, Content: row.Content, At: at})
	}
	conv := chatdomain.NewConversation(userID, maxTurns, turns)
	return conv, nil
}

func (r *JSONContextRepository) SaveConversation(ctx context.Context, c chatdomain.Conversation) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	rows := make([]storedTurn, 0, len(c.Turns))
	for _, turn := range c.Turns {
		rows = append(rows, storedTurn{Role: turn.Role, Content: turn.Content, At: turn.At.Format(time.RFC3339)})
	}
	r.data[c.UserID] = rows
	return r.persistLocked()
}

func (r *JSONContextRepository) ClearConversation(ctx context.Context, userID string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, userID)
	return r.persistLocked()
}

func (r *JSONContextRepository) loadFromDisk() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			r.data = map[string][]storedTurn{}
			return nil
		}
		return err
	}
	if len(b) == 0 {
		r.data = map[string][]storedTurn{}
		return nil
	}
	var parsed map[string][]storedTurn
	if err := json.Unmarshal(b, &parsed); err != nil {
		backup := r.path + ".corrupt." + time.Now().Format("20060102_150405")
		_ = os.WriteFile(backup, b, 0o644)
		r.data = map[string][]storedTurn{}
		return nil
	}
	r.data = parsed
	return nil
}

func (r *JSONContextRepository) persistLocked() error {
	tmp := r.path + ".tmp"
	b, err := json.MarshalIndent(r.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
