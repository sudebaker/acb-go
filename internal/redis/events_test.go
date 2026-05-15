package redis

import (
	"encoding/json"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

const TestRedisAddr = "localhost:16379"

func setupRedisClient() *goredis.Client {
	opts := &goredis.Options{
		Addr: TestRedisAddr,
	}
	return goredis.NewClient(opts)
}

func TestPublishTaskEvent_NilClient(t *testing.T) {
	// nil-safe: should not panic
	var p *Publisher
	p.PublishTaskEvent(EventNewTask, "t001", "agent-a", "", "")

	p = NewPublisher(nil)
	p.PublishTaskEvent(EventNewTask, "t001", "agent-a", "", "")
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