package domain

import "time"

type Turn struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

type Conversation struct {
	UserID   string
	MaxTurns int
	Turns    []Turn
}

func NewConversation(userID string, maxTurns int, turns []Turn) Conversation {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	c := Conversation{UserID: userID, MaxTurns: maxTurns, Turns: append([]Turn(nil), turns...)}
	c.Trim()
	return c
}

func (c *Conversation) AppendUser(content string, now time.Time) {
	c.Turns = append(c.Turns, Turn{Role: "user", Content: content, At: now})
	c.Trim()
}

func (c *Conversation) AppendAssistant(content string, now time.Time) {
	c.Turns = append(c.Turns, Turn{Role: "assistant", Content: content, At: now})
	c.Trim()
}

func (c *Conversation) Trim() {
	if c.MaxTurns <= 0 {
		return
	}
	if len(c.Turns) <= c.MaxTurns {
		return
	}
	c.Turns = c.Turns[len(c.Turns)-c.MaxTurns:]
}
