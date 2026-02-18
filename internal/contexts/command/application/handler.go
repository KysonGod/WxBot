package application

import (
	"context"
	"strings"

	cmddomain "wxbot/internal/contexts/command/domain"
)

type ContextCleaner interface {
	ClearConversation(ctx context.Context, userID string) error
}

type Result struct {
	Handled bool
	Reply   string
}

type Handler struct {
	Cleaner ContextCleaner
}

func (h *Handler) Handle(ctx context.Context, userID, rawText string) (Result, error) {
	cmd := extractCommand(rawText)
	if cmd == "" {
		return Result{}, nil
	}
	norm := cmddomain.Normalize(cmd)
	switch norm {
	case cmddomain.CmdPing:
		return Result{Handled: true, Reply: "pong"}, nil
	case cmddomain.CmdHelp:
		return Result{Handled: true, Reply: "可用命令: /ping, /help, /clear"}, nil
	case cmddomain.CmdClear:
		if h.Cleaner != nil {
			if err := h.Cleaner.ClearConversation(ctx, userID); err != nil {
				return Result{Handled: true, Reply: "清理上下文失败，请稍后再试。"}, err
			}
		}
		return Result{Handled: true, Reply: "已清理当前会话上下文。"}, nil
	default:
		return Result{}, nil
	}
}

func extractCommand(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "[群聊消息-") {
		idx := strings.Index(text, "]:")
		if idx >= 0 && idx+2 < len(text) {
			text = strings.TrimSpace(text[idx+2:])
		}
	}
	first := text
	if strings.Contains(text, "\n") {
		first = strings.SplitN(text, "\n", 2)[0]
	}
	first = strings.TrimSpace(first)
	if strings.HasPrefix(first, "/") {
		return first
	}
	return ""
}
