package application

import (
	"fmt"
	"strings"

	"wxbot/internal/shared/config"
)

const (
	logPreviewMax       = 180
	logReplyPreviewMax  = 320
	logReferenceMax     = 220
	logIncomingRawMax   = 180
	logIncomingFinalMax = 220
)

func (s *Service) logFlow(chatID, format string, args ...any) {
	id := strings.TrimSpace(chatID)
	if id == "" {
		id = "-"
	}
	params := make([]any, 0, len(args)+1)
	params = append(params, id)
	params = append(params, args...)
	s.logger.Printf("[flow][chat=%s] "+format, params...)
}

func textPreview(raw string, max int) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	if clean == "" {
		return ""
	}
	if max <= 0 {
		max = logPreviewMax
	}
	r := []rune(clean)
	if len(r) <= max {
		return clean
	}
	return fmt.Sprintf("%s...(len=%d)", string(r[:max]), len(r))
}

func modelLabel(m config.ModelConfig) string {
	provider := strings.TrimSpace(m.Provider)
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(m.Model)
	if model == "" {
		model = "unknown"
	}
	return provider + "/" + model
}
