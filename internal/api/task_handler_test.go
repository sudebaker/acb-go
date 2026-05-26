package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const testToken = "test-token"

func getTestDSN() string {
	return dbTestDSN()
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := getTestDSN()
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Ping(); err != nil {
		t.Fatal(err)
	}
	// Clean all tables before each test for isolation
	apiCleanTables(t, d)
	if err := db.RunMigrations(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		apiCleanTables(t, d)
		d.Close()
	})
	return d
}

func apiCleanTables(t *testing.T, d *sql.DB) {
	t.Helper()
	tables := []string{"task_events", "gates", "agents", "tasks"}
	for _, table := range tables {
		_, err := d.Exec(fmt.Sprintf(`
			DO $$
			BEGIN
				DELETE FROM %s;
			EXCEPTION WHEN undefined_table THEN NULL;
			END $$;
		`, table))
		if err != nil {
			// Table may not exist yet (first run before migrations)
			t.Logf("apiCleanTables: could not clean %s: %v", table, err)
		}
	}
	_, _ = d.Exec(`
		DO $$
		BEGIN
			DELETE FROM schema_version;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)
}

func setupRouter(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	d := setupTestDB(t)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})
	r := NewRouter(taskRepo, gateRepo, agentRepo, nil, nil, nil, nil, nil, nil)
	return d, r
}

func authRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

func TestCreateTask_201(t *testing.T) {
	_, r := setupRouter(t)
	req := authRequest("POST", "/tasks", `{"id":"t001","title":"test task","priority":3}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "pending" {
		t.Errorf("expected status pending, got %v", resp["status"])
	}
}

func TestCreateTask_WithSkills_201(t *testing.T) {
	_, r := setupRouter(t)
	req := authRequest("POST", "/tasks", `{"id":"t002","title":"skill test","skills":["python","go"],"required_skills":["python"],"tags":["api","web"]}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	// Verify skills are preserved
	skills, _ := resp["skills"].([]interface{})
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skills))
	}
}

func TestCreateTask_MissingTitle_400(t *testing.T) {
	_, r := setupRouter(t)
	req := authRequest("POST", "/tasks", `{"id":"t001"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test task", BodyGoal: "goal"})

	req := authRequest("GET", "/tasks/t001", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetTask_404(t *testing.T) {
	_, r := setupRouter(t)
	req := authRequest("GET", "/tasks/nonexistent", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestListTasks_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "task1"})
	taskRepo.Create(context.Background(), &models.Task{ID: "t002", Title: "task2"})

	req := authRequest("GET", "/tasks", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp []map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(resp))
	}
}

func TestListTasks_FilteredByRequiredSkills_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	// Create task with required skills
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "python task", RequiredSkills: []string{"python"}})
	taskRepo.Create(context.Background(), &models.Task{ID: "t002", Title: "java task", RequiredSkills: []string{"java"}})

	req := authRequest("GET", "/tasks?required_skills=python", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp []map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 || resp[0]["id"] != "t001" {
		t.Errorf("expected 1 task with id t001, got %v", resp)
	}
}

func TestClaimTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})

	req := authRequest("POST", "/tasks/t001/claim", `{"assignee":"worker-a"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClaimTask_AlreadyClaimed_409(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask(context.Background(), "t001", "worker-a")

	req := authRequest("POST", "/tasks/t001/claim", `{"assignee":"worker-b"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Errorf("expected 409, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if cs, ok := resp["current_status"]; !ok || cs != "claimed" {
		t.Errorf("expected current_status 'claimed', got %v", resp)
	}
}

func TestStartTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask(context.Background(), "t001", "worker-a")

	req := authRequest("POST", "/tasks/t001/start", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStartTask_WrongState_409(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})

	req := authRequest("POST", "/tasks/t001/start", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Errorf("expected 409, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if cs, ok := resp["current_status"]; !ok || cs != "pending" {
		t.Errorf("expected current_status 'pending', got %v", resp)
	}
}

func TestBlockTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask(context.Background(), "t001", "worker-a")
	taskRepo.StartTask(context.Background(), "t001")

	req := authRequest("POST", "/tasks/t001/block", `{"gate_id":"g001","question":"Should we proceed?"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask(context.Background(), "t001", "worker-a")
	taskRepo.StartTask(context.Background(), "t001")

	req := authRequest("POST", "/tasks/t001/complete", `{"summary":"all done"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFailTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask(context.Background(), "t001", "worker-a")
	taskRepo.StartTask(context.Background(), "t001")

	req := authRequest("POST", "/tasks/t001/fail", `{"reason":"something broke"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFailTask_WrongState_409(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})

	req := authRequest("POST", "/tasks/t001/fail", `{"reason":"not started"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if cs, ok := resp["current_status"]; !ok || cs != "pending" {
		t.Errorf("expected current_status 'pending', got %v", resp)
	}
}

func TestUnblockTask_200(t *testing.T) {
	d, r := setupRouter(t)
	gateRepo := db.NewGateRepo(d)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(context.Background(), &models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask(context.Background(), "t001", "worker-a")
	taskRepo.StartTask(context.Background(), "t001")
	taskRepo.UpdateStatus(context.Background(), "t001", "blocked")
	gateRepo.CreateGate(context.Background(), &models.Gate{GateID: "g001", TaskID: "t001", Question: "Q?"})
	gateRepo.AskGate(context.Background(), "g001", "")
	gateRepo.AnswerGate(context.Background(), "g001", "yes")

	req := authRequest("POST", "/tasks/t001/unblock", `{"gate_id":"g001"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListTaskEvents_200(t *testing.T) {
	_, r := setupRouter(t)
	req := authRequest("POST", "/tasks", `{"id":"t-events-1","title":"event test task"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create task: expected 201, got %d", w.Code)
	}

	req = authRequest("GET", "/tasks/t-events-1/events", "")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("list events: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var events []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event (CreateTask)")
	}
	// Most recent event first (ORDER BY timestamp DESC)
	if events[0]["event"] != "CreateTask" {
		t.Errorf("expected event 'CreateTask', got %v", events[0]["event"])
	}
}

func TestListTaskEvents_EmptyForNonexistent(t *testing.T) {
	_, r := setupRouter(t)
	req := authRequest("GET", "/tasks/nonexistent/events", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 for non-existent task events, got %d", w.Code)
	}
}

func TestDispatchNext_MatchingTask(t *testing.T) {
	d := setupTestDB(t)
	taskRepo := db.NewTaskRepo(d)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken, Skills: []string{"go", "testing"}})
	r := NewRouter(taskRepo, db.NewGateRepo(d), agentRepo, nil, nil, nil, nil, nil, nil)

	// Create a task matching the agent's skills
	taskRepo.Create(context.Background(), &models.Task{ID: "dispatch-1", Title: "go task", RequiredSkills: []string{"go"}, Priority: 5})

	req := httptest.NewRequest("GET", "/tasks/dispatch?agent=test-agent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] != "dispatch-1" {
		t.Errorf("expected task id 'dispatch-1', got %v", resp["id"])
	}
}

func TestDispatchNext_NoMatchingTask(t *testing.T) {
	d := setupTestDB(t)
	taskRepo := db.NewTaskRepo(d)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken, Skills: []string{"python"}})
	r := NewRouter(taskRepo, db.NewGateRepo(d), agentRepo, nil, nil, nil, nil, nil, nil)

	// Create a task requiring "rust" — agent doesn't have it
	taskRepo.Create(context.Background(), &models.Task{ID: "dispatch-2", Title: "rust task", RequiredSkills: []string{"rust"}})

	req := httptest.NewRequest("GET", "/tasks/dispatch?agent=test-agent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("expected 204 No Content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDispatchNext_NoPendingTasks(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken, Skills: []string{"go"}})
	r := NewRouter(db.NewTaskRepo(d), db.NewGateRepo(d), agentRepo, nil, nil, nil, nil, nil, nil)

	// No tasks created at all — should return 204
	req := httptest.NewRequest("GET", "/tasks/dispatch?agent=test-agent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("expected 204 No Content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDispatchNext_UnknownAgent(t *testing.T) {
	d := setupTestDB(t)
	taskRepo := db.NewTaskRepo(d)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})
	r := NewRouter(taskRepo, db.NewGateRepo(d), agentRepo, nil, nil, nil, nil, nil, nil)

	taskRepo.Create(context.Background(), &models.Task{ID: "dispatch-3", Title: "task"})

	req := httptest.NewRequest("GET", "/tasks/dispatch?agent=unknown-agent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Unknown agent: the dispatcher function returns nil task → 204
	if w.Code != 204 {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}