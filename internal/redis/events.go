package redis

import (
	"context"
	"encoding/json"
	"log"

	goredis "github.com/redis/go-redis/v9"
)

const (
	ChannelPrefix = "agent:"

	EventNewTask     = "new_task"
	EventTaskClaimed = "task_claimed"
	EventTaskStarted = "task_started"
	EventTaskBlocked = "task_blocked"
	EventTaskUnblock = "task_unblocked"
	EventTaskDone    = "task_completed"
	EventTaskFailed  = "task_failed"
)

type TaskEvent struct {
	Event   string `json:"event"`
	TaskID  string `json:"task_id"`
	Agent   string `json:"agent,omitempty"`
	GateID  string `json:"gate_id,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type Publisher struct {
	client *goredis.Client
}

func NewPublisher(client *goredis.Client) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) PublishTaskEvent(event, taskID, agent, gateID, summary string) {
	if p == nil || p.client == nil {
		return
	}

	msg := TaskEvent{
		Event:   event,
		TaskID:  taskID,
		Agent:   agent,
		GateID:  gateID,
		Summary: summary,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("redis: marshal event: %v", err)
		return
	}

	channel := ChannelPrefix + agent
	if err := p.client.Publish(context.Background(), channel, string(data)).Err(); err != nil {
		log.Printf("redis: publish to %s: %v", channel, err)
	}
}
