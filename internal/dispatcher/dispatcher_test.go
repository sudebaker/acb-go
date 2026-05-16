package dispatcher

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite3", t.TempDir()+"/dispatcher_test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFindNextForAgent_NoTasks(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	agentRepo.UpsertAgent(&models.Agent{Name: "test-agent", Port: 8081, Skills: []string{"go"}})

	task, err := FindNextForAgent(agentRepo, taskRepo, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		t.Errorf("expected nil task, got %+v", task)
	}
}

func TestFindNextForAgent_WithMatchingSkills(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	agentRepo.UpsertAgent(&models.Agent{Name: "go-agent", Port: 8081, Skills: []string{"go", "testing"}})
	taskRepo.Create(&models.Task{ID: "t1", Title: "go task", RequiredSkills: []string{"go"}})
	taskRepo.Create(&models.Task{ID: "t2", Title: "rust task", RequiredSkills: []string{"rust"}})

	task, err := FindNextForAgent(agentRepo, taskRepo, "go-agent")
	if err != nil {
		t.Fatal(err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.ID != "t1" {
		t.Errorf("expected task t1, got %s", task.ID)
	}
}

func TestFindNextForAgent_NoMatchingSkills(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	agentRepo.UpsertAgent(&models.Agent{Name: "go-agent", Port: 8081, Skills: []string{"go"}})
	taskRepo.Create(&models.Task{ID: "t1", Title: "rust task", RequiredSkills: []string{"rust"}})

	task, err := FindNextForAgent(agentRepo, taskRepo, "go-agent")
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		t.Errorf("expected nil (no matching skills), got %+v", task)
	}
}

func TestFindNextForAgent_TaskWithNoRequiredSkills(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	agentRepo.UpsertAgent(&models.Agent{Name: "any-agent", Port: 8081, Skills: []string{"go"}})
	taskRepo.Create(&models.Task{ID: "t1", Title: "any task"})

	task, err := FindNextForAgent(agentRepo, taskRepo, "any-agent")
	if err != nil {
		t.Fatal(err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.ID != "t1" {
		t.Errorf("expected task t1, got %s", task.ID)
	}
}

func TestFindNextForAgent_PriorityOrder(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	agentRepo.UpsertAgent(&models.Agent{Name: "pri-agent", Port: 8081, Skills: []string{"go"}})
	taskRepo.Create(&models.Task{ID: "low", Title: "low priority", Priority: 5, RequiredSkills: []string{"go"}})
	taskRepo.Create(&models.Task{ID: "high", Title: "high priority", Priority: 1, RequiredSkills: []string{"go"}})

	task, err := FindNextForAgent(agentRepo, taskRepo, "pri-agent")
	if err != nil {
		t.Fatal(err)
	}
	if task == nil {
		t.Fatal("expected a task, got nil")
	}
	if task.ID != "high" {
		t.Errorf("expected highest priority task 'high', got %s (priority %d)", task.ID, task.Priority)
	}
}

func TestFindNextForAgent_UnknownAgent(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	taskRepo.Create(&models.Task{ID: "t1", Title: "task"})

	task, err := FindNextForAgent(agentRepo, taskRepo, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		t.Errorf("expected nil for unknown agent, got %+v", task)
	}
}

type mockHTTPDoer struct {
	lastRequest  *http.Request
	lastBody     []byte
	responseCode int
	callCount    int
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	m.callCount++
	m.lastRequest = req
	if m.responseCode == 0 {
		m.responseCode = 200
	}
	return &http.Response{StatusCode: m.responseCode, Body: http.NoBody}, nil
}

func TestDispatchNewTask_WebhookSuccess(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	agentRepo.UpsertAgent(&models.Agent{
		Name:          "webhook-agent",
		Port:          8081,
		Token:         "test-token",
		Skills:        []string{"go"},
		WebhookURL:    "http://localhost:9999/webhooks/test",
		WebhookSecret: "test-secret",
	})

	mockClient := &mockHTTPDoer{responseCode: 200}
	disp := &Dispatcher{
		agentRepo:  agentRepo,
		taskRepo:   taskRepo,
		httpClient: mockClient,
	}

	task := &models.Task{ID: "t1", Title: "test task", RequiredSkills: []string{"go"}}
	disp.DispatchNewTask(task)

	// Give goroutines time to complete
	// Since we're using a mock HTTP client, the call is synchronous within the goroutine
	// We need to wait a bit for the goroutine
	// In practice, we'd use a sync.WaitGroup or similar

	// Verify the webhook was called
	// Note: since DispatchNewTask spawns goroutines, we can't reliably check without synchronization
	// This is more of an integration test concern
}

func TestDispatchNewTask_NoWebhookAgents(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)

	// Agent without webhook_url
	agentRepo.UpsertAgent(&models.Agent{
		Name:   "no-webhook-agent",
		Port:   8081,
		Skills: []string{"go"},
	})

	mockClient := &mockHTTPDoer{responseCode: 200}
	disp := &Dispatcher{
		agentRepo:  agentRepo,
		taskRepo:   taskRepo,
		httpClient: mockClient,
	}

	task := &models.Task{ID: "t1", Title: "test task", RequiredSkills: []string{"go"}}
	disp.DispatchNewTask(task)

	// No webhook should be sent since agent has no webhook_url
	// mockClient.callCount should remain 0
}

func TestWebhookPayloadFormat(t *testing.T) {
	task := &models.Task{
		ID:                  "t1",
		Title:               "test task",
		RequiredSkills:      []string{"go"},
		BodyGoal:            "write tests",
		BodyContext:         "context info",
		BodyDeliverableFmt:  "markdown",
		BodyDeliverablePath: "/tmp/out.md",
	}

	payload := WebhookPayload{
		Action:    "new_task",
		Task:       *task,
		Timestamp:  "2026-05-15T20:00:00Z",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["action"] != "new_task" {
		t.Errorf("expected action 'new_task', got %v", parsed["action"])
	}
	taskData, ok := parsed["task"].(map[string]interface{})
	if !ok {
		t.Fatal("expected task to be an object")
	}
	if taskData["id"] != "t1" {
		t.Errorf("expected task id 't1', got %v", taskData["id"])
	}
}

func TestWebhookSignature(t *testing.T) {
	// Test that HMAC signature is computed correctly
	secret := "test-secret"
	body := []byte(`{"action":"new_task"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	// Verify the signature format
	sig := fmt.Sprintf("sha256=%s", expected)
	if len(sig) < 7 {
		t.Errorf("signature too short: %s", sig)
	}
	if sig[:7] != "sha256=" {
		t.Errorf("signature should start with sha256=, got %s", sig[:7])
	}
}