package application

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	chatdomain "wxbot/internal/contexts/chat/domain"
	"wxbot/internal/shared/clock"
	"wxbot/internal/shared/config"
)

type queuedUser struct {
	ChatID        string
	SenderName    string
	Messages      []string
	LastMessageAt time.Time
}

type Service struct {
	cfg          config.AppConfig
	robotName    string
	wx           WeChatGateway
	llm          LLMPort
	assistantLLM LLMPort
	onlineLLM    LLMPort
	visionLLM    VisionPort
	contexts     ConversationRepository
	prompts      PromptRepository
	commands     CommandPort
	logger       *log.Logger
	clock        clock.Clock
	queueWait    time.Duration
	queueMax     int
	queueWorkers int
	policy       chatdomain.GroupPolicy

	mu       sync.Mutex
	queues   map[string]*queuedUser
	inFlight map[string]struct{}
}

func NewService(
	cfg config.AppConfig,
	robotName string,
	wx WeChatGateway,
	llmClient LLMPort,
	assistantLLM LLMPort,
	onlineLLM LLMPort,
	visionLLM VisionPort,
	contexts ConversationRepository,
	prompts PromptRepository,
	commands CommandPort,
	clk clock.Clock,
	logger *log.Logger,
) *Service {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if logger == nil {
		logger = log.Default()
	}
	if assistantLLM == nil {
		assistantLLM = llmClient
	}
	if onlineLLM == nil {
		onlineLLM = llmClient
	}
	p := chatdomain.GroupPolicy{
		AcceptAll:           cfg.GroupPolicy.AcceptAll,
		EnableAtReply:       cfg.GroupPolicy.EnableAtReply,
		EnableKeywordReply:  cfg.GroupPolicy.EnableKeywordReply,
		Keywords:            cfg.GroupPolicy.KeywordList,
		ResponseProbability: cfg.GroupPolicy.ResponseProbability,
		RobotName:           robotName,
	}
	return &Service{
		cfg:          cfg,
		robotName:    robotName,
		wx:           wx,
		llm:          llmClient,
		assistantLLM: assistantLLM,
		onlineLLM:    onlineLLM,
		visionLLM:    visionLLM,
		contexts:     contexts,
		prompts:      prompts,
		commands:     commands,
		logger:       logger,
		clock:        clk,
		queueWait:    time.Duration(cfg.QueueWaitingTime) * time.Second,
		queueMax:     cfg.QueueMaxMessages,
		queueWorkers: cfg.QueueWorkers,
		policy:       p,
		queues:       make(map[string]*queuedUser),
		inFlight:     make(map[string]struct{}),
	}
}

func (s *Service) EnqueueIncoming(ctx context.Context, msg chatdomain.IncomingMessage) {
	if strings.EqualFold(strings.TrimSpace(msg.Attr), "self") {
		return
	}
	chatID := strings.TrimSpace(msg.ChatID)
	sender := strings.TrimSpace(msg.Sender)
	msgType := strings.ToLower(strings.TrimSpace(msg.MsgType))
	if msgType == "" {
		msgType = "text"
	}

	rawContent := strings.TrimSpace(msg.Content)
	s.logFlow(chatID, "recv sender=%s type=%s attr=%s raw=%s",
		sender, msgType, strings.TrimSpace(msg.Attr), textPreview(rawContent, logIncomingRawMax))

	content, ok := s.preprocessIncomingContent(ctx, msg, rawContent)
	if !ok {
		s.logFlow(chatID, "drop empty content after preprocess")
		return
	}
	s.logFlow(chatID, "normalized content=%s", textPreview(content, logIncomingFinalMax))

	shouldProcess := false
	processedContent := content
	if msg.IsGroup {
		shouldProcess, processedContent = chatdomain.ShouldProcess(msg, s.policy)
	} else {
		if strings.EqualFold(strings.TrimSpace(msg.Attr), "friend") {
			shouldProcess = true
		}
	}
	if !shouldProcess {
		s.logFlow(chatID, "ignored by routing policy group=%t content=%s",
			msg.IsGroup, textPreview(processedContent, logPreviewMax))
		return
	}

	if s.cfg.EnableTextCommands && s.commands != nil {
		if res, err := s.commands.Handle(ctx, msg.ChatID, processedContent); res.Handled {
			if err != nil {
				s.logger.Printf("command handling warning for %s: %v", msg.ChatID, err)
			}
			s.logFlow(chatID, "command handled reply_non_empty=%t", strings.TrimSpace(res.Reply) != "")
			if strings.TrimSpace(res.Reply) != "" {
				_ = s.sendImmediate(ctx, msg.ChatID, res.Reply)
			}
			return
		}
	}

	if msg.IsGroup {
		processedContent = fmt.Sprintf("[群聊消息-来自群'%s'-发送者:%s]:%s", msg.ChatID, msg.Sender, processedContent)
	}
	stamped := fmt.Sprintf("[%s] %s", s.clock.Now().Format("2006-01-02 Monday 15:04:05"), processedContent)

	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queues[msg.ChatID]
	if !ok {
		q = &queuedUser{ChatID: msg.ChatID, SenderName: msg.Sender}
		s.queues[msg.ChatID] = q
	}
	q.Messages = append(q.Messages, stamped)
	if s.queueMax > 0 && len(q.Messages) > s.queueMax {
		dropped := len(q.Messages) - s.queueMax
		q.Messages = q.Messages[dropped:]
		s.logFlow(chatID, "queue overflow dropped=%d", dropped)
	}
	q.LastMessageAt = s.clock.Now()
	if q.SenderName == "" {
		q.SenderName = msg.Sender
	}
	s.logFlow(chatID, "queued sender=%s pending=%d latest=%s",
		q.SenderName, len(q.Messages), textPreview(processedContent, logPreviewMax))
}

func (s *Service) sendImmediate(ctx context.Context, chatID, reply string) error {
	parts := splitReply(reply)
	if len(parts) == 0 {
		s.logFlow(chatID, "reply skipped because split result is empty")
		return nil
	}
	s.logFlow(chatID, "reply split parts=%d mode=immediate", len(parts))
	for _, part := range parts {
		s.logFlow(chatID, "reply part=%s", textPreview(part, logReplyPreviewMax))
		if err := s.wx.SendText(ctx, chatID, part); err != nil {
			return err
		}
	}
	s.logFlow(chatID, "reply send done parts=%d mode=immediate", len(parts))
	return nil
}
