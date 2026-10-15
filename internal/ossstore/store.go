package ossstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/xiaozhuai/ossync/internal/config"
	"github.com/xiaozhuai/ossync/internal/syncer"
)

const maxSimpleUploadSize = int64(5 * 1024 * 1024 * 1024)

type Store struct {
	client *oss.Client
	bucket string
	prefix string
}

func New(cfg config.Config, target config.Target) *Store {
	provider := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)
	ossConfig := oss.LoadDefaultConfig().
		WithCredentialsProvider(provider).
		WithRegion(cfg.Region)
	return &Store{
		client: oss.NewClient(ossConfig),
		bucket: target.Bucket,
		prefix: target.Prefix,
	}
}

func (s *Store) List(ctx context.Context) ([]syncer.RemoteObject, error) {
	request := &oss.ListObjectsV2Request{
		Bucket: oss.Ptr(s.bucket),
		Prefix: oss.Ptr(s.prefix),
	}
	paginator := s.client.NewListObjectsV2Paginator(request)
	objects := make([]syncer.RemoteObject, 0)
	for paginator.HasNext() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list oss://%s/%s: %w", s.bucket, s.prefix, err)
		}
		for _, item := range page.Contents {
			key := oss.ToString(item.Key)
			if !strings.HasPrefix(key, s.prefix) {
				continue
			}
			checksum := strings.Trim(oss.ToString(item.ETag), "\"")
			if strings.Contains(checksum, "-") {
				checksum, err = s.multipartChecksum(ctx, key)
				if err != nil {
					return nil, err
				}
			}
			objects = append(objects, syncer.RemoteObject{
				Path: strings.TrimPrefix(key, s.prefix),
				Size: item.Size,
				MD5:  checksum,
			})
		}
	}
	return objects, nil
}

func (s *Store) Upload(ctx context.Context, relativePath, filename, md5sum string) error {
	request := &oss.PutObjectRequest{
		Bucket:   oss.Ptr(s.bucket),
		Key:      oss.Ptr(s.key(relativePath)),
		Metadata: map[string]string{"ossync-md5": md5sum},
	}
	info, err := fileInfo(filename)
	if err != nil {
		return err
	}
	if info.Size() <= maxSimpleUploadSize {
		_, err = s.client.PutObjectFromFile(ctx, request, filename)
		return err
	}
	uploader := s.client.NewUploader()
	_, err = uploader.UploadFile(ctx, request, filename)
	return err
}

func (s *Store) DeleteBatch(ctx context.Context, relativePaths []string) error {
	objects := make([]oss.ObjectIdentifier, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		objects = append(objects, oss.ObjectIdentifier{Key: oss.Ptr(s.key(relativePath))})
	}
	_, err := s.client.DeleteMultipleObjects(ctx, &oss.DeleteMultipleObjectsRequest{
		Bucket: oss.Ptr(s.bucket),
		Delete: &oss.Delete{
			Objects: objects,
			Quiet:   true,
		},
	})
	return err
}

func (s *Store) multipartChecksum(ctx context.Context, key string) (string, error) {
	result, err := s.client.HeadObject(ctx, &oss.HeadObjectRequest{
		Bucket: oss.Ptr(s.bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return "", fmt.Errorf("read metadata for oss://%s/%s: %w", s.bucket, key, err)
	}
	for name, value := range result.Metadata {
		if strings.EqualFold(name, "ossync-md5") {
			return value, nil
		}
	}
	// An object created outside ossync has no trusted full-object MD5. Returning
	// the multipart ETag as-is intentionally schedules one replacement.
	return strings.Trim(oss.ToString(result.ETag), "\""), nil
}

func (s *Store) key(relativePath string) string {
	return s.prefix + strings.TrimPrefix(relativePath, "/")
}
