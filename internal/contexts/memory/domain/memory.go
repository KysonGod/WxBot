package domain

import "time"

type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	Summary    string    `json:"summary"`
	Importance int       `json:"importance"`
}
