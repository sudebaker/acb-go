package rustfs

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("object not found")
var errNotFound = ErrNotFound

type ObjectStore interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, string, int64, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	BucketExists(ctx context.Context) (bool, error)
	MakeBucket(ctx context.Context) error
}

type Client struct {
	store   ObjectStore
	bucket  string
	enabled bool
}

func NewClientWithStore(store ObjectStore, bucket string) *Client {
	return &Client{store: store, bucket: bucket, enabled: true}
}

func NewClient(endpoint, region, accessKey, secretKey, bucket string) *Client {
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return &Client{enabled: false}
	}
	store := newMinioStore(endpoint, region, accessKey, secretKey, bucket)
	if store == nil {
		return &Client{enabled: false}
	}
	return &Client{store: store, bucket: bucket, enabled: true}
}

func (c *Client) enabledOp() ObjectStore {
	if !c.enabled {
		return nil
	}
	return c.store
}

func (c *Client) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	s := c.enabledOp()
	if s == nil {
		return nil
	}
	return s.Upload(ctx, key, reader, size, contentType)
}

func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	s := c.enabledOp()
	if s == nil {
		return nil, "", 0, ErrNotFound
	}
	return s.Download(ctx, key)
}

func (c *Client) Delete(ctx context.Context, key string) error {
	s := c.enabledOp()
	if s == nil {
		return nil
	}
	return s.Delete(ctx, key)
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	s := c.enabledOp()
	if s == nil {
		return false, nil
	}
	return s.Exists(ctx, key)
}

func (c *Client) Bucket() string {
	return c.bucket
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	s := c.enabledOp()
	if s == nil {
		return nil
	}
	exists, err := s.BucketExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return s.MakeBucket(ctx)
	}
	return nil
}
