package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	chatdomain "wxbot/internal/contexts/chat/domain"
)

func (s *Service) preprocessIncomingContent(ctx context.Context, msg chatdomain.IncomingMessage, raw string) (string, bool) {
	msgType := strings.ToLower(strings.TrimSpace(msg.MsgType))
	content := strings.TrimSpace(raw)

	switch msgType {
	case "voice":
		if msg.EventID != "" {
			text, err := s.wx.MessageToText(ctx, msg.EventID)
			if err != nil {
				s.logger.Printf("voice to text failed for %s: %v", msg.ChatID, err)
			} else if strings.TrimSpace(text) != "" {
				content = "[语音消息]: " + strings.TrimSpace(text)
			}
		}
	case "link":
		if msg.EventID != "" {
			url, err := s.wx.MessageGetURL(ctx, msg.EventID)
			if err != nil {
				s.logger.Printf("extract link failed for %s: %v", msg.ChatID, err)
			} else if strings.TrimSpace(url) != "" {
				content = "[卡片链接]: " + strings.TrimSpace(url)
			}
		}
	case "image":
		desc, err := s.recognizeMedia(ctx, msg.ChatID, msg.EventID, false)
		if err != nil {
			s.logger.Printf("image recognition failed for %s: %v", msg.ChatID, err)
		}
		if strings.TrimSpace(desc) != "" {
			content = "发送了图片：" + strings.TrimSpace(desc)
		} else if content == "" {
			content = "[图片]"
		}
	case "emotion":
		desc, err := s.recognizeMedia(ctx, msg.ChatID, msg.EventID, true)
		if err != nil {
			s.logger.Printf("emoji recognition failed for %s: %v", msg.ChatID, err)
		}
		if strings.TrimSpace(desc) != "" {
			content = "发送了表情包：" + strings.TrimSpace(desc)
		} else if content == "" {
			content = "[动画表情]"
		}
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}
	return content, true
}

func (s *Service) recognizeMedia(ctx context.Context, chatID, eventID string, isEmoji bool) (string, error) {
	if !s.cfg.Vision.Enabled || s.visionLLM == nil {
		return "", nil
	}
	if strings.TrimSpace(eventID) == "" {
		return "", fmt.Errorf("event id is empty")
	}

	var (
		imagePath string
		err       error
	)
	if isEmoji {
		imagePath, err = s.wx.MessageCapture(ctx, eventID)
	} else {
		imagePath, err = s.wx.MessageDownload(ctx, eventID)
	}
	if err != nil {
		return "", err
	}
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return "", fmt.Errorf("media path is empty")
	}
	defer func() {
		if shouldCleanupMediaPath(imagePath) {
			_ = os.Remove(imagePath)
		}
	}()

	prompt := s.cfg.Vision.ImagePrompt
	if isEmoji {
		prompt = s.cfg.Vision.EmojiPrompt
	}
	timeout := time.Duration(s.cfg.VisionModel.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	kind := "image"
	if isEmoji {
		kind = "emoji"
	}
	s.logFlow(chatID, "vision call kind=%s model=%s", kind, modelLabel(s.cfg.VisionModel))

	text, err := s.visionLLM.RecognizeImage(reqCtx, imagePath, prompt)
	if err != nil {
		return "", err
	}
	s.logFlow(chatID, "vision result kind=%s content=%s", kind, textPreview(text, logPreviewMax))
	return strings.TrimSpace(text), nil
}

func shouldCleanupMediaPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(abs))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
	default:
		return false
	}

	cleanAbs := strings.ToLower(filepath.Clean(abs))
	tempRoot := strings.ToLower(filepath.Clean(os.TempDir()))
	if strings.HasPrefix(cleanAbs, tempRoot+string(os.PathSeparator)) {
		return true
	}

	dirLower := strings.ToLower(filepath.Dir(cleanAbs))
	if strings.Contains(dirLower, "wxautox文件下载") ||
		strings.Contains(dirLower, "wxauto") ||
		strings.Contains(dirLower, "temp") {
		return true
	}
	return false
}
