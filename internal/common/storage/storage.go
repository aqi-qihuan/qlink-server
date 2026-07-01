package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage defines the interface for file storage backends.
type Storage interface {
	// Upload stores a file and returns its public URL.
	Upload(objectKey string, reader io.Reader, contentType string) (string, error)
	// Delete removes a file.
	Delete(objectKey string) error
	// GetURL returns the public URL for an object key.
	GetURL(objectKey string) string
}

// LocalStorage stores files on the local filesystem.
type LocalStorage struct {
	basePath string // e.g. "/data/uploads"
	baseURL  string // e.g. "http://localhost:8001/uploads"
}

func NewLocalStorage(basePath, baseURL string) *LocalStorage {
	os.MkdirAll(basePath, 0755)
	return &LocalStorage{basePath: basePath, baseURL: baseURL}
}

func (s *LocalStorage) Upload(objectKey string, reader io.Reader, contentType string) (string, error) {
	fullPath := filepath.Join(s.basePath, objectKey)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create dir failed: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file failed: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, reader); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}

	return s.GetURL(objectKey), nil
}

func (s *LocalStorage) Delete(objectKey string) error {
	fullPath := filepath.Join(s.basePath, objectKey)
	return os.Remove(fullPath)
}

func (s *LocalStorage) GetURL(objectKey string) string {
	return strings.TrimRight(s.baseURL, "/") + "/" + strings.TrimLeft(objectKey, "/")
}

// MinIOStorage stores files in MinIO using the official minio-go SDK.
type MinIOStorage struct {
	client    *minio.Client
	bucket    string
	publicURL string // e.g. "http://192.168.192.21:9000/aqicloud"
}

func NewMinIOStorage(endpoint, bucket, accessKey, secretKey string, useSSL bool, publicURL string) *MinIOStorage {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Printf("MinIO client init failed: %v", err)
		return &MinIOStorage{bucket: bucket, publicURL: publicURL}
	}
	return &MinIOStorage{client: client, bucket: bucket, publicURL: publicURL}
}

func (s *MinIOStorage) Upload(objectKey string, reader io.Reader, contentType string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("minio client not initialized")
	}

	// Read data to determine size
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read data failed: %w", err)
	}

	opts := minio.PutObjectOptions{
		ContentType: contentType,
	}

	_, err = s.client.PutObject(
		context.Background(),
		s.bucket,
		objectKey,
		bytes.NewReader(data),
		int64(len(data)),
		opts,
	)
	if err != nil {
		return "", fmt.Errorf("upload failed: %w", err)
	}

	return s.GetURL(objectKey), nil
}

func (s *MinIOStorage) Delete(objectKey string) error {
	if s.client == nil {
		return fmt.Errorf("minio client not initialized")
	}

	err := s.client.RemoveObject(
		context.Background(),
		s.bucket,
		objectKey,
		minio.RemoveObjectOptions{},
	)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	return nil
}

func (s *MinIOStorage) GetURL(objectKey string) string {
	return strings.TrimRight(s.publicURL, "/") + "/" + strings.TrimLeft(objectKey, "/")
}

// GenerateObjectKey creates a unique object key based on date and hash.
func GenerateObjectKey(originalFilename string, hash string) string {
	ext := filepath.Ext(originalFilename)
	date := time.Now().Format("2006/01/02")
	return fmt.Sprintf("user/%s/%s%s", date, hash, ext)
}
