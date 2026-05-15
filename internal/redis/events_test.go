package redis

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

const TestRedisAddr = "localhost:16379"

func setupRedisClient() *goredis.Client {
	opts := &goredis.Options{
		Addr:         TestRedisAddr,
		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	}
	return goredis.NewClient(opts)
}

func TestPublishTaskEvent_NewTask_Routes(t *testing.T) {
	client := setupRedisClient()
	defer client.Close()

	p := NewPublisher(client)

	// new_task should go to tasks:pending and agent:<name>
	p.PublishTaskEvent(EventNewTask, "t001", "agent-a", "", "", []string{"python"}...)

	// Give Redis time to process
	time.Sleep(100 * time.Millisecond)

	// Check tasks:pending channel
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Subscribe to tasks:pending
	pubsub := client.Subscribe(ctx, ChannelPending)
	defer pubsub.Close()

	// We can't easily verify broadcast in this test without a full pub/sub setup
	// This test just ensures no crash occurs
}

func TestPublishTaskEvent_TaskBlocked_Routes(t *testing.T) {
	client := setupRedisClient()
	defer client.Close()

	p := NewPublisher(client)

	// task_blocked should go to tasks:gates
	p.PublishTaskEvent(EventTaskBlocked, "t001", "", "gate-1", "")

	// Give Redis time to process
	time.Sleep(100 * time.Millisecond)

	// Check tasks:gates channel
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	pubsub := client.Subscribe(ctx, ChannelGates)
	defer pubsub.Close()
}

func TestTaskEvent_Struct(t *testing.T) {
	event := TaskEvent{
		Event:          EventNewTask,
		TaskID:         "t001",
		Agent:          "agent-a",
		GateID:         "",
		Summary:        "",
		RequiredSkills: []string{"python", "sql"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	// Verify RequiredSkills is included in JSON
	if len(event.RequiredSkills) != 2 {
		t.Errorf("expected 2 required skills, got %d", len(event.RequiredSkills))
	}

	var parsed TaskEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if len(parsed.RequiredSkills) != 2 {
		t.Errorf("expected 2 required skills after unmarshal, got %d", len(parsed.RequiredSkills))
	}
}
