package application

import (
	"context"

	chatdomain "wxbot/internal/contexts/chat/domain"
	cmdapp "wxbot/internal/contexts/command/application"
	"wxbot/internal/integration/llm"
)

type WeChatGateway interface {
	SendText(ctx context.Context, who, text string) error
	SendFile(ctx context.Context, who, path string) error
	MessageDownload(ctx context.Context, eventID string) (string, error)
	MessageCapture(ctx context.Context, eventID string) (string, error)
	MessageToText(ctx context.Context, eventID string) (string, error)
	MessageGetURL(ctx context.Context, eventID string) (string, error)
}

type LLMPort interface {
	Complete(ctx context.Context, messages []llm.Message) (string, error)
}

type VisionPort interface {
	RecognizeImage(ctx context.Context, imagePath, prompt string) (string, error)
}

type ConversationRepository interface {
	LoadConversation(ctx context.Context, userID string, maxTurns int) (chatdomain.Conversation, error)
	SaveConversation(ctx context.Context, c chatdomain.Conversation) error
	ClearConversation(ctx context.Context, userID string) error
}

type PromptRepository interface {
	GetPrompt(ctx context.Context, userID string) (string, error)
}

type CommandPort interface {
	Handle(ctx context.Context, userID, rawText string) (cmdapp.Result, error)
}
