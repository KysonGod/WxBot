package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"wxbot/internal/shared/config"
)

type OpenAICompatClient struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxTokens   int
	retries     int
	client      *http.Client
}

func NewOpenAICompatClient(cfg config.ModelConfig) *OpenAICompatClient {
	timeout := 45 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	// Accept either raw key or "Bearer xxx" to reduce misconfiguration.
	if strings.HasPrefix(strings.ToLower(apiKey), "bearer ") {
		apiKey = strings.TrimSpace(apiKey[7:])
	}
	return &OpenAICompatClient{
		baseURL:     strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:      apiKey,
		model:       strings.TrimSpace(cfg.Model),
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		retries:     2,
		client:      &http.Client{Timeout: timeout},
	}
}

type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAICompatClient) Complete(ctx context.Context, messages []Message) (string, error) {
	if c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return "", errors.New("llm config is incomplete")
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		text, err := c.completeOnce(ctx, messages)
		if err == nil {
			return stripThinkTags(text), nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 350 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("unknown llm error")
	}
	return "", lastErr
}

func (c *OpenAICompatClient) completeOnce(ctx context.Context, messages []Message) (string, error) {
	reqBody := chatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: c.temperature,
		MaxTokens:   c.maxTokens,
		Stream:      false,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal llm request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build llm request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call llm api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read llm response: %w", err)
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode llm response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("llm status %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("llm returned no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("llm returned empty content")
	}
	return content, nil
}

func stripThinkTags(s string) string {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)
	idx := strings.Index(lower, "</think>")
	if idx == -1 {
		idx = strings.Index(lower, "</thought>")
	}
	if idx == -1 {
		return trimmed
	}
	end := idx + len("</think>")
	if strings.HasPrefix(lower[idx:], "</thought>") {
		end = idx + len("</thought>")
	}
	if end >= len(trimmed) {
		return ""
	}
	return strings.TrimSpace(trimmed[end:])
}
