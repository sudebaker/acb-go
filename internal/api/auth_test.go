package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
)

func authTestRouter(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(&models.Agent{Name: "worker-a", Token: "valid-token"})

	r := chi.NewRouter()
	r.Use(AuthMiddleware(agentRepo))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"status": "ok"})
	})
	r.Get("/tasks", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"agent": r.Header.Get("X-Agent-Name")})
	})

	return d, r
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	_, r := authTestRouter(t)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	_, r := authTestRouter(t)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	_, r := authTestRouter(t)
	req := httptest.NewRequest("GET", "/tasks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_HealthBypass(t *testing.T) {
	_, r := authTestRouter(t)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
