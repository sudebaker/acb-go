package models

import "time"

type Task struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title"`
	Assignee            string     `json:"assignee"`
	Status              string     `json:"status"`
	Priority            int        `json:"priority"`
	Parents             []string   `json:"parents"`
	Skills              []string   `json:"skills,omitempty"`
	RequiredSkills      []string   `json:"required_skills,omitempty"`
	Tags                []string   `json:"tags,omitempty"`
	BodyGoal            string     `json:"body_goal"`
	BodyContext         string     `json:"body_context"`
	BodyDeliverableFmt  string     `json:"body_deliverable_format"`
	BodyDeliverablePath string     `json:"body_deliverable_path"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Summary             string     `json:"summary"`
	Artifacts           []Artifact `json:"artifacts"`
}

type TaskEvent struct {
	TaskID    string `json:"task_id"`
	Event     string `json:"event"`
	Agent     string `json:"agent"`
	Timestamp string `json:"timestamp"`
	Detail    string `json:"detail,omitempty"`
}

type Artifact struct {
	Key         string `json:"key"`
	Bucket      string `json:"bucket"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}
