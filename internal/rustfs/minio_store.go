package rustfs

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioStore struct {
	client *minio.Client
	bucket string
}

func newMinioStore(endpoint, region, accessKey, secretKey, bucket string) *minioStore {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
		Region: region,
	})
	if err != nil {
		return nil
	}
	return &minioStore{client: client, bucket: bucket}
}

func (s *minioStore) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("upload to rustfs: %w", err)
	}
	return nil
}

func (s *minioStore) Download(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", 0, fmt.Errorf("download from rustfs: %w", err)
	}

	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return nil, "", 0, errNotFound
		}
		return nil, "", 0, fmt.Errorf("stat object: %w", err)
	}

	return obj, stat.ContentType, stat.Size, nil
}

func (s *minioStore) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("delete from rustfs: %w", err)
	}
	return nil
}

func (s *minioStore) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("stat object: %w", err)
	}
	return true, nil
}

func (s *minioStore) BucketExists(ctx context.Context) (bool, error) {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return false, fmt.Errorf("check bucket: %w", err)
	}
	return exists, nil
}

func (s *minioStore) MakeBucket(ctx context.Context) error {
	err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}
