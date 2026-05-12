package storage

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Storage interface {
	Upload(ctx context.Context, data []byte) (string, error)
}

type minioStorage struct {
	client   *minio.Client
	bucket   string
	endpoint string
}

func NewMinioStorage(endpoint, accessKey, secretKey, bucket string) (Storage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}
	return &minioStorage{client: client, bucket: bucket, endpoint: endpoint}, nil
}

func (s *minioStorage) Upload(ctx context.Context, data []byte) (string, error) {
	objectName := uuid.New().String() + ".png"
	_, err := s.client.PutObject(ctx, s.bucket, objectName,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "image/png"},
	)
	if err != nil {
		return "", fmt.Errorf("uploading to minio: %w", err)
	}
	return fmt.Sprintf("http://%s/%s/%s", s.endpoint, s.bucket, objectName), nil
}
