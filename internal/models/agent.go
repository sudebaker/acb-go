package models

import "time"

type Agent struct {
	Name          string     `json:"name"`
	Port          int        `json:"port"`
	Token         string     `json:"token,omitempty"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	Skills        []string   `json:"skills,omitempty"`
	WebhookURL    string     `json:"webhook_url,omitempty"`
	WebhookSecret string     `json:"webhook_secret,omitempty"`
}