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

type moonshotMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	Name       string             `json:"name,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []moonshotToolCall `json:"tool_calls,omitempty"`
}

type moonshotToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function moonshotToolFunction `json:"function"`
}

type moonshotToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type moonshotTool struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

type moonshotChatRequest struct {
	Model       string            `json:"model"`
	Messages    []moonshotMessage `json:"messages"`
	Tools       []moonshotTool    `json:"tools,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream"`
}

type moonshotChatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string             `json:"role"`
			Content   string             `json:"content"`
			ToolCalls []moonshotToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func CompleteWithMoonshotWebSearch(ctx context.Context, cfg config.ModelConfig, userPrompt string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	apiKey := strings.TrimSpace(cfg.APIKey)
	model := strings.TrimSpace(cfg.Model)
	if strings.HasPrefix(strings.ToLower(apiKey), "bearer ") {
		apiKey = strings.TrimSpace(apiKey[7:])
	}
	if baseURL == "" || apiKey == "" || model == "" {
		return "", errors.New("moonshot web-search model config is incomplete")
	}

	timeout := 45 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	client := &http.Client{Timeout: timeout}

	tools := []moonshotTool{
		{
			Type: "builtin_function",
			Function: struct {
				Name string `json:"name"`
			}{
				Name: "$web_search",
			},
		},
	}

	messages := []moonshotMessage{
		{
			Role:    "user",
			Content: strings.TrimSpace(userPrompt),
		},
	}

	maxRounds := 3
	for round := 0; round < maxRounds; round++ {
		reqBody := moonshotChatRequest{
			Model:       model,
			Messages:    messages,
			Tools:       tools,
			Temperature: cfg.Temperature,
			MaxTokens:   cfg.MaxTokens,
			Stream:      false,
		}
		resp, err := doMoonshotChatCompletion(ctx, client, baseURL, apiKey, reqBody)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("moonshot returned no choices")
		}

		choice := resp.Choices[0]
		content := strings.TrimSpace(choice.Message.Content)
		finishReason := strings.ToLower(strings.TrimSpace(choice.FinishReason))

		if content != "" && finishReason != "tool_calls" {
			return stripThinkTags(content), nil
		}

		if len(choice.Message.ToolCalls) == 0 {
			if content != "" {
				return stripThinkTags(content), nil
			}
			return "", errors.New("moonshot web-search returned empty content")
		}

		messages = append(messages, moonshotMessage{
			Role:      "assistant",
			Content:   choice.Message.Content,
			ToolCalls: choice.Message.ToolCalls,
		})
		for _, tc := range choice.Message.ToolCalls {
			toolContent := strings.TrimSpace(tc.Function.Arguments)
			if toolContent == "" {
				toolContent = "{}"
			}
			messages = append(messages, moonshotMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    toolContent,
			})
		}
	}

	return "", errors.New("moonshot web-search did not finish within max rounds")
}

func doMoonshotChatCompletion(
	ctx context.Context,
	client *http.Client,
	baseURL, apiKey string,
	reqBody moonshotChatRequest,
) (moonshotChatResponse, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return moonshotChatResponse{}, fmt.Errorf("marshal moonshot request failed: %w", err)
	}

	url := baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return moonshotChatResponse{}, fmt.Errorf("build moonshot request failed: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return moonshotChatResponse{}, fmt.Errorf("call moonshot failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return moonshotChatResponse{}, fmt.Errorf("read moonshot response failed: %w", err)
	}

	var parsed moonshotChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return moonshotChatResponse{}, fmt.Errorf("decode moonshot response failed: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return moonshotChatResponse{}, fmt.Errorf("moonshot status %d: %s", httpResp.StatusCode, parsed.Error.Message)
		}
		return moonshotChatResponse{}, fmt.Errorf("moonshot status %d", httpResp.StatusCode)
	}
	return parsed, nil
}

func IsMoonshotProvider(cfg config.ModelConfig) bool {
	p := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if strings.Contains(p, "moonshot") || strings.Contains(p, "kimi") {
		return true
	}
	base := strings.ToLower(strings.TrimSpace(cfg.BaseURL))
	return strings.Contains(base, "moonshot.cn") || strings.Contains(base, "moonshot.ai")
}
