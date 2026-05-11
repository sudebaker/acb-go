package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amphora/acb/internal/db"
	"github.com/amphora/acb/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite3", t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func setupRouter(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	d := setupTestDB(t)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)
	agentRepo := db.NewAgentRepo(d)
	r := NewRouter(taskRepo, gateRepo, agentRepo, nil)
	return d, r
}

func TestCreateTask_201(t *testing.T) {
	_, r := setupRouter(t)
	body := `{"id":"t001","title":"test task","priority":3}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestCreateTask_MissingTitle_400(t *testing.T) {
	_, r := setupRouter(t)
	body := `{"id":"t001"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test task", BodyGoal: "goal"})

	req := httptest.NewRequest("GET", "/tasks/t001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetTask_404(t *testing.T) {
	_, r := setupRouter(t)
	req := httptest.NewRequest("GET", "/tasks/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestListTasks_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "task1"})
	taskRepo.Create(&models.Task{ID: "t002", Title: "task2"})

	req := httptest.NewRequest("GET", "/tasks", nil)
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

func TestClaimTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	body := `{"assignee":"worker-a"}`
	req := httptest.NewRequest("POST", "/tasks/t001/claim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClaimTask_AlreadyClaimed_409(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask("t001", "worker-a")

	body := `{"assignee":"worker-b"}`
	req := httptest.NewRequest("POST", "/tasks/t001/claim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestStartTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask("t001", "worker-a")

	req := httptest.NewRequest("POST", "/tasks/t001/start", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStartTask_WrongState_409(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	req := httptest.NewRequest("POST", "/tasks/t001/start", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestBlockTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask("t001", "worker-a")
	taskRepo.StartTask("t001")

	body := `{"gate_id":"g001","question":"Should we proceed?"}`
	req := httptest.NewRequest("POST", "/tasks/t001/block", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask("t001", "worker-a")
	taskRepo.StartTask("t001")

	body := `{"summary":"all done"}`
	req := httptest.NewRequest("POST", "/tasks/t001/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFailTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	body := `{"reason":"something broke"}`
	req := httptest.NewRequest("POST", "/tasks/t001/fail", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUnblockTask_200(t *testing.T) {
	d, r := setupRouter(t)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})
	taskRepo.ClaimTask("t001", "worker-a")
	taskRepo.StartTask("t001")
	taskRepo.UpdateStatus("t001", "blocked")
	gateRepo.CreateGate(&models.Gate{GateID: "g001", TaskID: "t001", Question: "Q?"})
	d.Exec("UPDATE gates SET status = 'asked' WHERE gate_id = 'g001'")
	gateRepo.AnswerGate("g001", "yes")

	body := `{"gate_id":"g001"}`
	req := httptest.NewRequest("POST", "/tasks/t001/unblock", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
