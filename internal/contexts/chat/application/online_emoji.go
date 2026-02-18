package application

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"wxbot/internal/integration/llm"
)

var (
	onlineJSONPattern = regexp.MustCompile(`(?s)\{.*\}`)
	emojiCleanPattern = regexp.MustCompile(`[^\p{Han}\p{L}\p{N}_-]+`)
	randSeedOnce      sync.Once
)

func (s *Service) maybeGetOnlineReference(ctx context.Context, merged, userID string) (string, error) {
	if !s.cfg.Online.Enabled || s.onlineLLM == nil {
		return "", nil
	}
	merged = strings.TrimSpace(merged)
	if merged == "" {
		return "", nil
	}

	detector := s.assistantLLM
	detectModel := s.cfg.AssistantModel
	if detector == nil {
		detector = s.llm
		detectModel = s.cfg.MainModel
	}
	if detector == nil {
		return "", nil
	}

	detectPrompt := strings.TrimSpace(s.cfg.Online.DetectionPrompt)
	if detectPrompt == "" {
		detectPrompt = "是否需要查询今天的天气、最新新闻、股价、实时事件、当前网站内容等外部实时信息"
	}
	s.logFlow(userID, "online detect call model=%s", modelLabel(detectModel))

	detectResp, err := detector.Complete(ctx, []llm.Message{
		{
			Role: "system",
			Content: "你是联网需求判断器。你只输出 JSON，且不要输出代码块。输出格式固定为：\n" +
				`{"need_online": true/false, "query": "..."}`,
		},
		{
			Role: "user",
			Content: fmt.Sprintf(
				"判断下面用户消息是否需要联网查询实时外部信息。\n判断参考：%s\n\n用户消息：%s\n\n只返回 JSON。",
				detectPrompt,
				merged,
			),
		},
	})
	if err != nil {
		return "", err
	}

	needOnline, query := parseOnlineDecision(detectResp, merged)
	s.logFlow(userID, "online decision need=%t query=%s", needOnline, textPreview(query, logPreviewMax))
	if !needOnline {
		return "", nil
	}
	if strings.TrimSpace(query) == "" {
		query = merged
	}

	currentTime := s.clock.Now().Format("2006-01-02 Monday 15:04:05")
	fixed := strings.TrimSpace(s.cfg.Online.FixedPrompt)
	var b strings.Builder
	b.WriteString("请根据用户问题检索并整理准确的参考信息。\n")
	if fixed != "" {
		b.WriteString(fixed)
		b.WriteString("\n")
	}
	b.WriteString("当前时间：")
	b.WriteString(currentTime)
	b.WriteString("\n用户原始消息：")
	b.WriteString(merged)
	b.WriteString("\n建议检索查询：")
	b.WriteString(query)
	b.WriteString("\n请输出简洁、事实导向的中文参考要点。")

	onlinePrompt := b.String()
	if llm.IsMoonshotProvider(s.cfg.OnlineModel) {
		s.logFlow(userID, "online call model=%s mode=web_search", modelLabel(s.cfg.OnlineModel))
		ref, err := llm.CompleteWithMoonshotWebSearch(ctx, s.cfg.OnlineModel, onlinePrompt)
		if err != nil {
			return "", fmt.Errorf("moonshot web search failed (user=%s): %w", userID, err)
		}
		s.logFlow(userID, "online result=%s", textPreview(ref, logReferenceMax))
		return strings.TrimSpace(ref), nil
	}

	s.logFlow(userID, "online call model=%s mode=chat", modelLabel(s.cfg.OnlineModel))
	ref, err := s.onlineLLM.Complete(ctx, []llm.Message{{Role: "user", Content: onlinePrompt}})
	if err != nil {
		return "", err
	}
	s.logFlow(userID, "online result=%s", textPreview(ref, logReferenceMax))
	return strings.TrimSpace(ref), nil
}

func parseOnlineDecision(raw, fallbackQuery string) (bool, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, ""
	}
	candidate := raw
	if m := onlineJSONPattern.FindString(raw); strings.TrimSpace(m) != "" {
		candidate = m
	}

	var parsed struct {
		NeedOnline any    `json:"need_online"`
		Need       any    `json:"need"`
		Query      string `json:"query"`
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
		need := parseNeedBool(parsed.NeedOnline)
		if !need {
			need = parseNeedBool(parsed.Need)
		}
		if need {
			q := strings.TrimSpace(parsed.Query)
			if q == "" {
				q = strings.TrimSpace(fallbackQuery)
			}
			return true, q
		}
		return false, ""
	}

	lower := strings.ToLower(raw)
	if strings.Contains(raw, "不需要联网") || strings.Contains(lower, "no_need_online") {
		return false, ""
	}
	if strings.Contains(raw, "需要联网") || strings.Contains(lower, "need_online") {
		lines := strings.Split(raw, "\n")
		if len(lines) > 1 {
			q := strings.TrimSpace(strings.Join(lines[1:], " "))
			if q != "" {
				return true, q
			}
		}
		return true, strings.TrimSpace(fallbackQuery)
	}
	return false, ""
}

func parseNeedBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "yes" || s == "1" || s == "需要联网"
	case float64:
		return x != 0
	default:
		return false
	}
}

func (s *Service) maybeSendEmojiByReply(ctx context.Context, chatID, reply string) {
	if !s.cfg.Emoji.Enabled || s.wx == nil {
		return
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	ensureRandSeeded()
	if rand.Intn(100)+1 > s.cfg.Emoji.SendProbability {
		return
	}

	category, err := s.detectEmojiCategory(ctx, chatID, reply)
	if err != nil {
		s.logger.Printf("emoji category detect failed for %s: %v", chatID, err)
		return
	}
	if category == "" {
		s.logFlow(chatID, "emoji skip reason=category_none")
		return
	}

	emojiPath, err := s.pickEmojiFile(category)
	if err != nil {
		s.logger.Printf("pick emoji file failed for %s: %v", chatID, err)
		return
	}
	s.logFlow(chatID, "emoji selected category=%s file=%s", category, filepath.Base(emojiPath))
	for attempt := 1; attempt <= 3; attempt++ {
		sendCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		err = s.wx.SendFile(sendCtx, chatID, emojiPath)
		cancel()
		if err == nil {
			s.logFlow(chatID, "emoji sent category=%s file=%s", category, filepath.Base(emojiPath))
			return
		}
		if attempt < 3 {
			s.clock.Sleep(time.Duration(attempt) * 350 * time.Millisecond)
		}
	}
	s.logger.Printf("send emoji failed for %s: %v", chatID, err)
}

func (s *Service) detectEmojiCategory(ctx context.Context, chatID, reply string) (string, error) {
	categories, err := s.listEmojiCategories()
	if err != nil {
		return "", err
	}
	if len(categories) == 0 {
		return "", nil
	}

	detector := s.assistantLLM
	detectModel := s.cfg.AssistantModel
	if detector == nil {
		detector = s.llm
		detectModel = s.cfg.MainModel
	}
	if detector == nil {
		return "", nil
	}

	prompt := fmt.Sprintf(
		"请判断以下机器人回复最适合搭配哪个表情分类。候选分类：%s。\n"+
			"只回复一个分类名；若不需要表情，请回复 None。\n机器人回复：%s",
		strings.Join(categories, ", "),
		reply,
	)
	s.logFlow(chatID, "emoji detect call model=%s categories=%d", modelLabel(detectModel), len(categories))
	resp, err := detector.Complete(ctx, []llm.Message{{Role: "user", Content: prompt}})
	if err != nil {
		return "", err
	}

	token := normalizeEmojiToken(resp)
	if token == "" || token == "none" || token == "无" || token == "不需要" {
		return "", nil
	}

	byNormalized := make(map[string]string, len(categories))
	for _, c := range categories {
		byNormalized[normalizeEmojiToken(c)] = c
	}
	if match := byNormalized[token]; match != "" {
		return match, nil
	}

	raw := strings.TrimSpace(resp)
	for _, c := range categories {
		if strings.Contains(raw, c) {
			return c, nil
		}
	}
	return "", nil
}

func (s *Service) listEmojiCategories() ([]string, error) {
	dir := strings.TrimSpace(s.cfg.Emoji.Dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := strings.TrimSpace(e.Name())
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) pickEmojiFile(category string) (string, error) {
	folder := filepath.Join(strings.TrimSpace(s.cfg.Emoji.Dir), category)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", err
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
			files = append(files, filepath.Join(folder, e.Name()))
		}
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no emoji files found in %s", folder)
	}
	ensureRandSeeded()
	return files[rand.Intn(len(files))], nil
}

func normalizeEmojiToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "\n") {
		raw = strings.TrimSpace(strings.Split(raw, "\n")[0])
	}
	raw = strings.Trim(raw, "`\"'[](){}<>。，,.;:：！？!?")
	raw = emojiCleanPattern.ReplaceAllString(raw, "")
	return strings.ToLower(strings.TrimSpace(raw))
}

func ensureRandSeeded() {
	randSeedOnce.Do(func() {
		rand.Seed(time.Now().UnixNano())
	})
}
