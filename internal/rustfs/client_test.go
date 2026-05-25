package rustfs

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
)

type memObject struct {
	content     []byte
	contentType string
}

type memoryStore struct {
	mu   sync.Mutex
	data map[string]*memObject
}

func newMemoryStore() *memoryStore {
	return &memoryStore{data: make(map[string]*memObject)}
}

func (s *memoryStore) Upload(_ context.Context, key string, reader io.Reader, _ int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = &memObject{content: data, contentType: contentType}
	return nil
}

func (s *memoryStore) Download(_ context.Context, key string) (io.ReadCloser, string, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, "", 0, errNotFound
	}
	return io.NopCloser(bytes.NewReader(obj.content)), obj.contentType, int64(len(obj.content)), nil
}

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *memoryStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *memoryStore) BucketExists(_ context.Context) (bool, error) {
	return true, nil
}

func (s *memoryStore) MakeBucket(_ context.Context) error {
	return nil
}

func (s *memoryStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for key := range s.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func TestNilClient_UploadNoop(t *testing.T) {
	c := &Client{enabled: false}
	if err := c.Upload(context.Background(), "k", strings.NewReader("x"), 1, "text/plain"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestNilClient_DownloadNoop(t *testing.T) {
	c := &Client{enabled: false}
	body, ct, size, err := c.Download(context.Background(), "k")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if body != nil {
		t.Error("expected nil body")
	}
	if ct != "" {
		t.Errorf("expected empty content-type, got %q", ct)
	}
	if size != 0 {
		t.Errorf("expected 0 size, got %d", size)
	}
}

func TestNilClient_DeleteNoop(t *testing.T) {
	c := &Client{enabled: false}
	if err := c.Delete(context.Background(), "k"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestNilClient_ExistsFalse(t *testing.T) {
	c := &Client{enabled: false}
	ok, err := c.Exists(context.Background(), "k")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if ok {
		t.Error("expected false for disabled client")
	}
}

func TestNilClient_EnsureBucketNoop(t *testing.T) {
	c := &Client{enabled: false}
	if err := c.EnsureBucket(context.Background()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestClient_UploadAndDownload(t *testing.T) {
	s := newMemoryStore()
	c := &Client{store: s, bucket: "test-bucket", enabled: true}

	content := "hello rustfs"
	key := "myfile.txt"
	if err := c.Upload(context.Background(), key, strings.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		t.Fatal(err)
	}

	body, ct, size, err := c.Download(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("got %q, want %q", string(data), content)
	}
	if ct != "text/plain" {
		t.Errorf("got content-type %q, want %q", ct, "text/plain")
	}
	if size != int64(len(content)) {
		t.Errorf("got size %d, want %d", size, len(content))
	}
}

func TestClient_Delete(t *testing.T) {
	s := newMemoryStore()
	c := &Client{store: s, bucket: "test-bucket", enabled: true}

	c.Upload(context.Background(), "k", strings.NewReader("x"), 1, "text/plain")
	if err := c.Delete(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}

	exists, _ := c.Exists(context.Background(), "k")
	if exists {
		t.Error("expected object to be deleted")
	}
}

func TestClient_Exists(t *testing.T) {
	s := newMemoryStore()
	c := &Client{store: s, bucket: "test-bucket", enabled: true}

	exists, err := c.Exists(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("expected false for nonexistent key")
	}

	c.Upload(context.Background(), "k", strings.NewReader("x"), 1, "text/plain")
	exists, err = c.Exists(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected true for existing key")
	}
}

func TestClient_ListObjects(t *testing.T) {
	s := newMemoryStore()
	c := &Client{store: s, bucket: "test-bucket", enabled: true}

	c.Upload(context.Background(), "task1/a.txt", strings.NewReader("hello"), 5, "text/plain")
	c.Upload(context.Background(), "task1/b.txt", strings.NewReader("world"), 5, "text/plain")
	c.Upload(context.Background(), "task2/c.txt", strings.NewReader("other"), 5, "text/plain")

	keys, err := c.ListObjects(context.Background(), "task1/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 objects, got %d", len(keys))
	}

	keys, err = c.ListObjects(context.Background(), "task2/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 object, got %d", len(keys))
	}

	keys, err = c.ListObjects(context.Background(), "nonexistent/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 objects, got %d", len(keys))
	}
}

func TestNilClient_ListObjects(t *testing.T) {
	c := &Client{enabled: false}
	_, err := c.ListObjects(context.Background(), "prefix/")
	if err == nil {
		t.Error("expected error for disabled client")
	}
}
