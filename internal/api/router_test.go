package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRouter(t *testing.T) {
	t.Run("health", func(t *testing.T) {
		r := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil)
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var body map[string]string
		json.NewDecoder(w.Body).Decode(&body)
		if body["status"] != "ok" {
			t.Errorf("expected status ok, got %q", body["status"])
		}
	})

	t.Run("unknown route returns 404", func(t *testing.T) {
		r := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil)
		req := httptest.NewRequest("GET", "/nonexistent", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 404 {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("content type is json", func(t *testing.T) {
		r := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil)
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		ct := w.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected application/json, got %q", ct)
		}
	})
}