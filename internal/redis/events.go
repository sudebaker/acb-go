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

	ChannelPending = "tasks:pending"
	ChannelGates   = "tasks:gates"
)

type TaskEvent struct {
	Event          string   `json:"event"`
	TaskID         string   `json:"task_id"`
	Agent          string   `json:"agent,omitempty"`
	GateID         string   `json:"gate_id,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	RequiredSkills []string `json:"required_skills,omitempty"`
}

type Publisher struct {
	client *goredis.Client
}

func NewPublisher(client *goredis.Client) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) PublishTaskEvent(event, taskID, agent, gateID, summary string, requiredSkills ...string) {
	if p == nil || p.client == nil {
		return
	}

	msg := TaskEvent{
		Event:          event,
		TaskID:         taskID,
		Agent:          agent,
		GateID:         gateID,
		Summary:        summary,
		RequiredSkills: requiredSkills,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("redis: marshal event: %v", err)
		return
	}

	// Route to correct channels based on event type
	var channels []string

	switch event {
	case EventNewTask:
		// new_task -> tasks:pending (broadcast) + agent:<assignee> if present
		channels = append(channels, ChannelPending)
		if agent != "" {
			channels = append(channels, ChannelPrefix+agent)
		}
	case EventTaskClaimed, EventTaskStarted:
		// task_claimed/task_started -> agent:<assignee>
		if agent != "" {
			channels = append(channels, ChannelPrefix+agent)
		}
	case EventTaskBlocked, EventTaskUnblock:
		// task_blocked/task_unblocked -> tasks:gates
		channels = append(channels, ChannelGates)
	case EventTaskDone, EventTaskFailed:
		// task_completed/task_failed -> agent:<assignee>
		if agent != "" {
			channels = append(channels, ChannelPrefix+agent)
		}
	}

	for _, channel := range channels {
		if err := p.client.Publish(context.Background(), channel, string(data)).Err(); err != nil {
			log.Printf("redis: publish to %s: %v", channel, err)
		}
	}
}
