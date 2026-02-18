package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	chatdomain "wxbot/internal/contexts/chat/domain"
	"wxbot/internal/integration/wxbridge"
)

func (a *App) Run(ctx context.Context) error {
	for _, row := range a.Config.ListenList {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := a.Bridge.AddListenChat(callCtx, row.Nickname)
		cancel()
		if err != nil {
			return fmt.Errorf("add listen chat %q failed: %w", row.Nickname, err)
		}
		a.Logger.Printf("listen chat added: %s", row.Nickname)
	}

	go a.Chat.StartQueueWorker(ctx)
	go a.heartbeatLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-a.Bridge.Events():
			if !ok {
				return fmt.Errorf("wx bridge event stream closed")
			}
			if evt.Event != "message" {
				continue
			}
			var m wxbridge.MessageEvent
			if err := json.Unmarshal(evt.Data, &m); err != nil {
				a.Logger.Printf("decode message event failed: %v", err)
				continue
			}
			incoming := toIncoming(m)
			a.Chat.EnqueueIncoming(ctx, incoming)
		}
	}
}

func (a *App) Close(ctx context.Context) {
	if a.Bridge != nil {
		a.Bridge.Shutdown(ctx)
		_ = a.Bridge.Close()
	}
}

func toIncoming(m wxbridge.MessageEvent) chatdomain.IncomingMessage {
	chatID := strings.TrimSpace(m.Who)
	sender := strings.TrimSpace(m.Sender)
	if chatID == "" {
		chatID = sender
	}
	isGroup := false
	if chatID != "" && sender != "" && !strings.EqualFold(chatID, sender) {
		isGroup = true
	}
	return chatdomain.IncomingMessage{
		ChatID:    chatID,
		Sender:    sender,
		Attr:      strings.TrimSpace(m.Attr),
		MsgType:   strings.TrimSpace(m.MsgType),
		Content:   m.Content,
		IsGroup:   isGroup,
		EventID:   m.EventID,
		RawWho:    m.Who,
		RawSender: m.Sender,
	}
}

func (a *App) heartbeatLoop(ctx context.Context) {
	url := fmt.Sprintf("http://localhost:%d/bot_heartbeat", a.Config.Port)
	payload := map[string]any{"pid": 0}
	client := &http.Client{Timeout: 4 * time.Second}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			body, _ := json.Marshal(payload)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
}
