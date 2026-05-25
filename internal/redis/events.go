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

// RouteChannels returns the Redis channels a given event type should be published to.
// This is extracted for testability so routing logic can be verified without a Redis server.
func RouteChannels(event, agent string) []string {
	var channels []string
	switch event {
	case EventNewTask:
		channels = append(channels, ChannelPending)
		if agent != "" {
			channels = append(channels, ChannelPrefix+agent)
		}
	case EventTaskClaimed, EventTaskStarted:
		if agent != "" {
			channels = append(channels, ChannelPrefix+agent)
		}
	case EventTaskBlocked, EventTaskUnblock:
		channels = append(channels, ChannelGates)
	case EventTaskDone, EventTaskFailed:
		if agent != "" {
			channels = append(channels, ChannelPrefix+agent)
		}
	}
	return channels
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

	channels := RouteChannels(event, agent)

	for _, channel := range channels {
		if err := p.client.Publish(context.Background(), channel, string(data)).Err(); err != nil {
			log.Printf("redis: publish to %s: %v", channel, err)
		}
	}
}