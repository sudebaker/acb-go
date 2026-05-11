package redis

import (
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestPublishTaskEvent_NilPublisher(t *testing.T) {
	var p *Publisher
	p.PublishTaskEvent(EventNewTask, "t001", "agent-a", "", "")
}

func TestPublishTaskEvent_NilClient(t *testing.T) {
	p := &Publisher{client: nil}
	p.PublishTaskEvent(EventNewTask, "t001", "agent-a", "", "")
}

func TestPublishTaskEvent_NoRedis(t *testing.T) {
	opts := &goredis.Options{
		Addr:         "localhost:16379",
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	}
	client := goredis.NewClient(opts)
	defer client.Close()

	p := NewPublisher(client)
	p.PublishTaskEvent(EventNewTask, "t001", "agent-a", "", "")
}
