package models

import "time"

type Gate struct {
	GateID     string     `json:"gate_id"`
	TaskID     string     `json:"task_id"`
	Question   string     `json:"question"`
	Ask        string     `json:"ask"`
	Status     string     `json:"status"`
	Answer     string     `json:"answer"`
	CreatedAt  time.Time  `json:"created_at"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
}
