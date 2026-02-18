package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionClient interface {
	Complete(ctx context.Context, messages []Message) (string, error)
}
