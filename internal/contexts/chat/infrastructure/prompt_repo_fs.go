package infrastructure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FSPromptRepository struct {
	promptsDir string
	mapping    map[string]string
}

func NewFSPromptRepository(promptsDir string, mapping map[string]string) *FSPromptRepository {
	copied := make(map[string]string, len(mapping))
	for k, v := range mapping {
		copied[k] = v
	}
	return &FSPromptRepository{promptsDir: promptsDir, mapping: copied}
}

func (r *FSPromptRepository) GetPrompt(ctx context.Context, userID string) (string, error) {
	_ = ctx
	promptName := strings.TrimSpace(r.mapping[userID])
	if promptName == "" {
		promptName = strings.TrimSpace(userID)
	}
	promptName = sanitizeName(promptName)
	if promptName == "" {
		return "你是一个乐于助人的助手。", nil
	}

	baseAbs, err := filepath.Abs(r.promptsDir)
	if err != nil {
		return "", fmt.Errorf("resolve prompts dir: %w", err)
	}
	target := filepath.Join(baseAbs, promptName+".md")
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve prompt path: %w", err)
	}
	if !strings.HasPrefix(targetAbs, baseAbs+string(os.PathSeparator)) && targetAbs != baseAbs {
		return "", fmt.Errorf("blocked path traversal: %s", targetAbs)
	}

	b, err := os.ReadFile(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return "你是一个乐于助人的助手。", nil
		}
		return "", err
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return "你是一个乐于助人的助手。", nil
	}
	return text, nil
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "..", "")
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")
	return strings.TrimSpace(name)
}
