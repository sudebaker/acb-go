package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	"github.com/sudebaker/acb-go/internal/rustfs"
)

type handlerMemStore struct {
	mu   sync.Mutex
	data map[string]*handlerMemObj
}

type handlerMemObj struct {
	content     []byte
	contentType string
}

func newHandlerMemStore() *handlerMemStore {
	return &handlerMemStore{data: make(map[string]*handlerMemObj)}
}

func (s *handlerMemStore) Upload(_ context.Context, key string, reader io.Reader, _ int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = &handlerMemObj{content: data, contentType: contentType}
	return nil
}

func (s *handlerMemStore) Download(_ context.Context, key string) (io.ReadCloser, string, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, "", 0, rustfs.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(obj.content)), obj.contentType, int64(len(obj.content)), nil
}

func (s *handlerMemStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *handlerMemStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *handlerMemStore) BucketExists(_ context.Context) (bool, error) {
	return true, nil
}

func (s *handlerMemStore) MakeBucket(_ context.Context) error {
	return nil
}

func (s *handlerMemStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for key := range s.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func setupRouterWithRustFS(t *testing.T) (*db.TaskRepo, *rustfs.Client, http.Handler, *handlerMemStore) {
	t.Helper()
	d := setupTestDB(t)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(&models.Agent{Name: "test-agent", Token: testToken})

	memStore := newHandlerMemStore()
	rustfsClient := rustfs.NewClientWithStore(memStore, "test-bucket")

	r := NewRouter(taskRepo, gateRepo, agentRepo, nil, rustfsClient)
	return taskRepo, rustfsClient, r, memStore
}

func TestUploadArtifact_201(t *testing.T) {
	taskRepo, _, r, _ := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fileWriter, err := w.CreateFormFile("file", "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	fileWriter.Write([]byte("hello rustfs"))
	w.Close()

	req := authRequest("POST", "/tasks/t001/artifacts", buf.String())
	req.Header.Set("Content-Type", w.FormDataContentType())
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 201 {
		t.Errorf("expected 201, got %d: %s", wResp.Code, wResp.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(wResp.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["key"] == nil {
		t.Error("expected key in response")
	}
	if resp["size"] != float64(12) {
		t.Errorf("expected size 12, got %v", resp["size"])
	}
}

func TestUploadArtifact_TaskNotFound_404(t *testing.T) {
	_, _, r, _ := setupRouterWithRustFS(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "report.txt")
	fw.Write([]byte("data"))
	w.Close()

	req := authRequest("POST", "/tasks/nonexistent/artifacts", buf.String())
	req.Header.Set("Content-Type", w.FormDataContentType())
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 404 {
		t.Errorf("expected 404, got %d", wResp.Code)
	}
}

func TestUploadArtifact_EmptyFile_400(t *testing.T) {
	taskRepo, _, r, _ := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "empty.txt")
	fw.Write([]byte{})
	w.Close()

	req := authRequest("POST", "/tasks/t001/artifacts", buf.String())
	req.Header.Set("Content-Type", w.FormDataContentType())
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 400 {
		t.Errorf("expected 400, got %d", wResp.Code)
	}
}

func TestListArtifacts_200(t *testing.T) {
	taskRepo, _, r, memStore := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	memStore.Upload(context.Background(), "t001/uuid-file1.txt", bytes.NewReader([]byte("a")), 1, "text/plain")
	taskRepo.AddArtifact("t001", models.Artifact{Key: "t001/uuid-file1.txt", Size: 1, ContentType: "text/plain"})

	req := authRequest("GET", "/tasks/t001/artifacts", "")
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 200 {
		t.Errorf("expected 200, got %d: %s", wResp.Code, wResp.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.NewDecoder(wResp.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(resp))
	}
}

func TestListArtifacts_Empty_200(t *testing.T) {
	taskRepo, _, r, _ := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	req := authRequest("GET", "/tasks/t001/artifacts", "")
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 200 {
		t.Errorf("expected 200, got %d", wResp.Code)
	}

	var resp []map[string]interface{}
	json.NewDecoder(wResp.Body).Decode(&resp)
	if len(resp) != 0 {
		t.Errorf("expected empty list, got %d", len(resp))
	}
}

func TestDownloadArtifact_200(t *testing.T) {
	taskRepo, _, r, memStore := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	memStore.Upload(context.Background(), "t001/uuid-file.txt", bytes.NewReader([]byte("hello")), 5, "text/plain")

	req := authRequest("GET", "/tasks/t001/artifacts?key=t001%2Fuuid-file.txt", "")
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 200 {
		t.Errorf("expected 200, got %d: %s", wResp.Code, wResp.Body.String())
	}

	body, _ := io.ReadAll(wResp.Body)
	if string(body) != "hello" {
		t.Errorf("expected 'hello', got %q", string(body))
	}
}

func TestDownloadArtifact_NotFound_404(t *testing.T) {
	taskRepo, _, r, _ := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	req := authRequest("GET", "/tasks/t001/artifacts?key=nonexistent", "")
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 404 {
		t.Errorf("expected 404, got %d", wResp.Code)
	}
}

func TestDeleteArtifact_204(t *testing.T) {
	taskRepo, _, r, memStore := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	memStore.Upload(context.Background(), "t001/uuid-file.txt", bytes.NewReader([]byte("x")), 1, "text/plain")
	taskRepo.AddArtifact("t001", models.Artifact{Key: "t001/uuid-file.txt", Size: 1, ContentType: "text/plain"})

	req := authRequest("DELETE", "/tasks/t001/artifacts?key=t001%2Fuuid-file.txt", "")
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 204 {
		t.Errorf("expected 204, got %d: %s", wResp.Code, wResp.Body.String())
	}

	req2 := authRequest("GET", "/tasks/t001/artifacts", "")
	wResp2 := httptest.NewRecorder()
	r.ServeHTTP(wResp2, req2)

	var artifacts []map[string]interface{}
	json.NewDecoder(wResp2.Body).Decode(&artifacts)
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts after delete, got %d", len(artifacts))
	}
}

func TestDeleteArtifact_NotFound_404(t *testing.T) {
	taskRepo, _, r, _ := setupRouterWithRustFS(t)
	taskRepo.Create(&models.Task{ID: "t001", Title: "test"})

	req := authRequest("DELETE", "/tasks/t001/artifacts?key=nonexistent", "")
	wResp := httptest.NewRecorder()
	r.ServeHTTP(wResp, req)

	if wResp.Code != 404 {
		t.Errorf("expected 404, got %d", wResp.Code)
	}
}
