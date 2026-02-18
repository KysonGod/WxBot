package domain

import "time"

type Kind string

const (
	KindOneOff    Kind = "one-off"
	KindRecurring Kind = "recurring"
)

type Reminder struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	Kind      Kind      `json:"kind"`
	TriggerAt time.Time `json:"trigger_at"`
}
