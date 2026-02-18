package application

import (
	"context"
	"strings"
	"time"

	chatdomain "wxbot/internal/contexts/chat/domain"
	"wxbot/internal/integration/llm"
)

func (s *Service) StartQueueWorker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	workerN := s.queueWorkers
	if workerN <= 0 {
		workerN = 1
	}
	sem := make(chan struct{}, workerN)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ready := s.pullReadyUsers(s.clock.Now())
			for _, q := range ready {
				if q == nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case sem <- struct{}{}:
				}
				go func(item *queuedUser) {
					defer func() {
						<-sem
						s.finishQueueProcessing(item.ChatID)
					}()
					s.processQueuedUser(ctx, item)
				}(q)
			}
		}
	}
}

func (s *Service) pullReadyUsers(now time.Time) []*queuedUser {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.queues) == 0 {
		return nil
	}
	out := make([]*queuedUser, 0, len(s.queues))
	for userID, q := range s.queues {
		if _, busy := s.inFlight[userID]; busy {
			continue
		}
		if now.Sub(q.LastMessageAt) < s.queueWait {
			continue
		}
		delete(s.queues, userID)
		s.inFlight[userID] = struct{}{}
		out = append(out, q)
	}
	return out
}

func (s *Service) finishQueueProcessing(chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, chatID)
}

func (s *Service) processQueuedUser(ctx context.Context, q *queuedUser) {
	if q == nil || len(q.Messages) == 0 {
		return
	}
	merged := strings.Join(q.Messages, " ")
	s.logFlow(q.ChatID, "processing start pending_messages=%d sender=%s", len(q.Messages), q.SenderName)
	s.logFlow(q.ChatID, "merged user input=%s", textPreview(merged, logIncomingFinalMax))

	prompt, err := s.prompts.GetPrompt(ctx, q.ChatID)
	if err != nil {
		s.logger.Printf("load prompt failed for %s: %v", q.ChatID, err)
		prompt = "你是一个乐于助人的助手。"
	}
	s.logFlow(q.ChatID, "prompt loaded chars=%d", runeLen(prompt))
	onlineRef, err := s.maybeGetOnlineReference(ctx, merged, q.ChatID)
	if err != nil {
		s.logger.Printf("online reference failed for %s: %v", q.ChatID, err)
	} else if strings.TrimSpace(onlineRef) != "" {
		s.logFlow(q.ChatID, "online reference=%s", textPreview(onlineRef, logReferenceMax))
	} else {
		s.logFlow(q.ChatID, "online reference skipped")
	}

	conv, err := s.contexts.LoadConversation(ctx, q.ChatID, s.cfg.MaxGroups*2)
	if err != nil {
		s.logger.Printf("load conversation failed for %s: %v", q.ChatID, err)
		conv = chatdomain.NewConversation(q.ChatID, s.cfg.MaxGroups*2, nil)
	}
	if strings.TrimSpace(conv.UserID) == "" {
		conv = chatdomain.NewConversation(q.ChatID, s.cfg.MaxGroups*2, conv.Turns)
	}
	s.logFlow(q.ChatID, "conversation history_turns=%d", len(conv.Turns))
	conv.AppendUser(merged, s.clock.Now())

	messages := make([]llm.Message, 0, 1+len(conv.Turns))
	messages = append(messages, llm.Message{Role: "system", Content: prompt})
	if strings.TrimSpace(onlineRef) != "" {
		messages = append(messages, llm.Message{
			Role: "system",
			Content: "以下是联网检索的参考信息（可能不完整），请结合它回答用户问题，但不要提及你进行了联网检索：\n---\n" +
				onlineRef + "\n---",
		})
	}
	for _, turn := range conv.Turns {
		messages = append(messages, llm.Message{Role: turn.Role, Content: turn.Content})
	}

	s.logFlow(q.ChatID, "llm call stage=assistant model=%s messages=%d",
		modelLabel(s.cfg.AssistantModel), len(messages))
	reply, err := s.assistantLLM.Complete(ctx, messages)
	if err != nil {
		s.logger.Printf("llm call failed for %s (model=%s): %v", q.ChatID, modelLabel(s.cfg.AssistantModel), err)
		reply = "抱歉，我现在有点忙，稍后再聊吧。"
	} else {
		s.logFlow(q.ChatID, "llm reply=%s", textPreview(reply, logReplyPreviewMax))
	}

	conv.AppendAssistant(reply, s.clock.Now())
	if err := s.contexts.SaveConversation(ctx, conv); err != nil {
		s.logger.Printf("save conversation failed for %s: %v", q.ChatID, err)
	}
	s.logFlow(q.ChatID, "conversation saved turns=%d", len(conv.Turns))

	s.sendReplyWithDelay(ctx, q.ChatID, reply)
	s.maybeSendEmojiByReply(ctx, q.ChatID, reply)
	s.logFlow(q.ChatID, "processing done")
}
