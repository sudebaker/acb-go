package tests_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudebaker/acb-go/internal/api"
	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

func TestFullTaskLifecycle(t *testing.T) {
	d, err := sql.Open("sqlite3", t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	db.RunMigrations(d)

	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)
	agentRepo := db.NewAgentRepo(d)

	agentRepo.UpsertAgent(&models.Agent{Name: "worker-a", Token: "e2e-token"})

	r := api.NewRouter(taskRepo, gateRepo, agentRepo, nil, nil, nil, nil)

	auth := func(method, target, body string) *http.Request {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer e2e-token")
		return req
	}

	exec := func(req *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// POST /tasks → 201, status=pending
	req := auth("POST", "/tasks", `{"id":"t001","title":"E2E test task","priority":2}`)
	w := exec(req)
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task map[string]interface{}
	json.NewDecoder(w.Body).Decode(&task)
	if task["status"] != "pending" {
		t.Fatalf("create: expected status pending, got %v", task["status"])
	}

	// POST /tasks/:id/claim → 200, status=claimed
	req = auth("POST", "/tasks/t001/claim", `{"assignee":"worker-a"}`)
	w = exec(req)
	if w.Code != 200 {
		t.Fatalf("claim: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&task)
	if task["status"] != "claimed" {
		t.Fatalf("claim: expected claimed, got %v", task["status"])
	}

	// POST /tasks/:id/start → 200, status=in_progress
	req = auth("POST", "/tasks/t001/start", "")
	w = exec(req)
	if w.Code != 200 {
		t.Fatalf("start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&task)
	if task["status"] != "in_progress" {
		t.Fatalf("start: expected in_progress, got %v", task["status"])
	}

	// POST /tasks/:id/block → 200, status=blocked
	req = auth("POST", "/tasks/t001/block", `{"gate_id":"g001","question":"Proceed?"}`)
	w = exec(req)
	if w.Code != 200 {
		t.Fatalf("block: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// POST /tasks/:id/unblock → 200, status=in_progress
	// Gate was created by block; transition it to answered
	d.Exec("UPDATE gates SET status = 'asked' WHERE gate_id = 'g001'")
	gateRepo.AnswerGate("g001", "yes")

	req = auth("POST", "/tasks/t001/unblock", `{"gate_id":"g001"}`)
	w = exec(req)
	if w.Code != 200 {
		t.Fatalf("unblock: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&task)
	if task["status"] != "in_progress" {
		t.Fatalf("unblock: expected in_progress, got %v", task["status"])
	}

	// POST /tasks/:id/complete → 200, status=completed
	req = auth("POST", "/tasks/t001/complete", `{"summary":"Test completed successfully"}`)
	w = exec(req)
	if w.Code != 200 {
		t.Fatalf("complete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&task)
	if task["status"] != "completed" {
		t.Fatalf("complete: expected completed, got %v", task["status"])
	}
}
