package models

type Agent struct {
	Name          string `json:"name"`
	Port          int    `json:"port"`
	Token         string `json:"token,omitempty"`
	LastHeartbeat string `json:"last_heartbeat,omitempty"`
}
