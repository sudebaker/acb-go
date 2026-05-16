package redis

import (
	"encoding/json"
	"testing"
)

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

// --- RouteChannels unit tests ---
// These test the routing logic without needing a real Redis server.

func TestRouteChannels_NewTask_WithAgent(t *testing.T) {
	channels := RouteChannels(EventNewTask, "braulio")
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels for new_task with agent, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelPending {
		t.Errorf("expected first channel to be %s, got %s", ChannelPending, channels[0])
	}
	if channels[1] != ChannelPrefix+"braulio" {
		t.Errorf("expected second channel to be agent:braulio, got %s", channels[1])
	}
}

func TestRouteChannels_NewTask_NoAgent(t *testing.T) {
	channels := RouteChannels(EventNewTask, "")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for new_task without agent, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelPending {
		t.Errorf("expected channel to be %s, got %s", ChannelPending, channels[0])
	}
}

func TestRouteChannels_TaskClaimed(t *testing.T) {
	channels := RouteChannels(EventTaskClaimed, "amanda")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for task_claimed, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelPrefix+"amanda" {
		t.Errorf("expected agent:amanda, got %s", channels[0])
	}
}

func TestRouteChannels_TaskClaimed_NoAgent(t *testing.T) {
	channels := RouteChannels(EventTaskClaimed, "")
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels for task_claimed without agent, got %d: %v", len(channels), channels)
	}
}

func TestRouteChannels_TaskStarted(t *testing.T) {
	channels := RouteChannels(EventTaskStarted, "braulio")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for task_started, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelPrefix+"braulio" {
		t.Errorf("expected agent:braulio, got %s", channels[0])
	}
}

func TestRouteChannels_TaskStarted_NoAgent(t *testing.T) {
	// This is the bug we fixed: task_started with no agent publishes to NO channels
	channels := RouteChannels(EventTaskStarted, "")
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels for task_started without agent, got %d: %v", len(channels), channels)
	}
}

func TestRouteChannels_TaskBlocked(t *testing.T) {
	channels := RouteChannels(EventTaskBlocked, "")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for task_blocked, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelGates {
		t.Errorf("expected %s, got %s", ChannelGates, channels[0])
	}
}

func TestRouteChannels_TaskUnblocked(t *testing.T) {
	channels := RouteChannels(EventTaskUnblock, "")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for task_unblocked, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelGates {
		t.Errorf("expected %s, got %s", ChannelGates, channels[0])
	}
}

func TestRouteChannels_TaskDone(t *testing.T) {
	channels := RouteChannels(EventTaskDone, "braulio")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for task_completed, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelPrefix+"braulio" {
		t.Errorf("expected agent:braulio, got %s", channels[0])
	}
}

func TestRouteChannels_TaskDone_NoAgent(t *testing.T) {
	// Bug we fixed: task_completed without agent publishes to NO channels
	channels := RouteChannels(EventTaskDone, "")
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels for task_completed without agent, got %d: %v", len(channels), channels)
	}
}

func TestRouteChannels_TaskFailed(t *testing.T) {
	channels := RouteChannels(EventTaskFailed, "armando")
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel for task_failed, got %d: %v", len(channels), channels)
	}
	if channels[0] != ChannelPrefix+"armando" {
		t.Errorf("expected agent:armando, got %s", channels[0])
	}
}

func TestRouteChannels_TaskFailed_NoAgent(t *testing.T) {
	channels := RouteChannels(EventTaskFailed, "")
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels for task_failed without agent, got %d: %v", len(channels), channels)
	}
}

func TestTaskEvent_Fields(t *testing.T) {
	// Verify that all fields are properly serialized, especially after fixing
	// the handler bugs where agent and requiredSkills were omitted
	event := TaskEvent{
		Event:          EventTaskDone,
		TaskID:         "abc-123",
		Agent:          "braulio",
		GateID:         "",
		Summary:        "Task completed successfully",
		RequiredSkills: []string{"coding"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	var parsed TaskEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.Event != EventTaskDone {
		t.Errorf("expected event %s, got %s", EventTaskDone, parsed.Event)
	}
	if parsed.TaskID != "abc-123" {
		t.Errorf("expected task_id abc-123, got %s", parsed.TaskID)
	}
	if parsed.Agent != "braulio" {
		t.Errorf("expected agent braulio, got %s", parsed.Agent)
	}
	if parsed.Summary != "Task completed successfully" {
		t.Errorf("expected summary 'Task completed successfully', got %s", parsed.Summary)
	}
	if len(parsed.RequiredSkills) != 1 || parsed.RequiredSkills[0] != "coding" {
		t.Errorf("expected required_skills [coding], got %v", parsed.RequiredSkills)
	}
}

func TestTaskEvent_OmitEmpty(t *testing.T) {
	// Verify omitempty works correctly for optional fields
	event := TaskEvent{
		Event:  EventTaskBlocked,
		TaskID: "t001",
		Agent:  "",
		GateID: "gate-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// Agent should be omitted when empty (omitempty)
	if _, exists := raw["agent"]; exists {
		t.Error("agent should be omitted when empty")
	}
	// GateID should be present when set
	if _, exists := raw["gate_id"]; !exists {
		t.Error("gate_id should be present when set")
	}
}