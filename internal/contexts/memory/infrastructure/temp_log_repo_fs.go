package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type FSTempLogRepository struct {
	baseDir string
}

func NewFSTempLogRepository(baseDir string) *FSTempLogRepository {
	return &FSTempLogRepository{baseDir: baseDir}
}

func (r *FSTempLogRepository) AppendUser(ctx context.Context, userID, text string) error {
	return r.append(ctx, userID, "user", text)
}

func (r *FSTempLogRepository) AppendAssistant(ctx context.Context, userID, text string) error {
	return r.append(ctx, userID, "assistant", text)
}

func (r *FSTempLogRepository) append(ctx context.Context, userID, role, text string) error {
	_ = ctx
	if err := os.MkdirAll(r.baseDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(r.baseDir, sanitize(userID)+"_log.txt")
	line := fmt.Sprintf("%s | [%s] %s\n", time.Now().Format("2006-01-02 Monday 15:04:05"), role, text)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
