package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wxbot/internal/shared/config"
)

type VisionClient struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxTokens   int
	client      *http.Client
}

func NewVisionClient(cfg config.ModelConfig) *VisionClient {
	timeout := 45 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	if strings.HasPrefix(strings.ToLower(apiKey), "bearer ") {
		apiKey = strings.TrimSpace(apiKey[7:])
	}
	return &VisionClient{
		baseURL:     strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:      apiKey,
		model:       strings.TrimSpace(cfg.Model),
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		client:      &http.Client{Timeout: timeout},
	}
}

func (v *VisionClient) RecognizeImage(ctx context.Context, imagePath, prompt string) (string, error) {
	if strings.TrimSpace(imagePath) == "" {
		return "", errors.New("image path is empty")
	}
	if v.baseURL == "" || v.apiKey == "" || v.model == "" {
		return "", errors.New("vision model config is incomplete")
	}

	b, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image failed: %w", err)
	}
	mimeType := detectImageMimeType(imagePath)
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(b)

	reqBody := map[string]any{
		"model":       v.model,
		"temperature": v.temperature,
		"max_tokens":  v.maxTokens,
		"stream":      false,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "image_url", "image_url": map[string]any{"url": dataURI}},
					{"type": "text", "text": prompt},
				},
			},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal vision request failed: %w", err)
	}

	url := v.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build vision request failed: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+v.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call vision api failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read vision response failed: %w", err)
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode vision response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return "", fmt.Errorf("vision status %d: %s", resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("vision status %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("vision returned no choices")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return "", errors.New("vision returned empty content")
	}
	return stripThinkTags(text), nil
}

func detectImageMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/jpeg"
	}
}
