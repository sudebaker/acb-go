package models

import "time"

type Task struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title"`
	Assignee            string     `json:"assignee"`
	Status              string     `json:"status"`
	Priority            int        `json:"priority"`
	Parents             []string   `json:"parents"`
	BodyGoal            string     `json:"body_goal"`
	BodyContext         string     `json:"body_context"`
	BodyDeliverableFmt  string     `json:"body_deliverable_format"`
	BodyDeliverablePath string     `json:"body_deliverable_path"`
	CreatedAt           time.Time  `json:"created_at"`
	Summary             string     `json:"summary"`
	Artifacts           []Artifact `json:"artifacts"`
}

type Artifact struct {
	Key         string `json:"key"`
	Bucket      string `json:"bucket"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}
