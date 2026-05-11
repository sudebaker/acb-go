package redis

import (
	"context"
	"encoding/json"

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
}

type Publisher struct {
	client *goredis.Client
}

func NewPublisher(client *goredis.Client) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) PublishTaskEvent(event, taskID, agent string) {
	if p == nil || p.client == nil {
		return
	}

	msg := TaskEvent{
		Event:  event,
		TaskID: taskID,
		Agent:  agent,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	channel := ChannelPrefix + agent
	p.client.Publish(context.Background(), channel, string(data))
}
