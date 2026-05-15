package models

type Gate struct {
	GateID      string  `json:"gate_id"`
	TaskID      string  `json:"task_id"`
	Question    string  `json:"question"`
	Ask         string  `json:"ask"`
	Status      string  `json:"status"`
	Answer      string  `json:"answer"`
	CreatedAt   string  `json:"created_at"`
	AnsweredAt  *string `json:"answered_at,omitempty"`
}
