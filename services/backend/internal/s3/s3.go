package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"zdorovie/backend/internal/config"
)

// Client wraps MinIO S3 operations including public-URL pre-signing through Nginx.
type Client struct {
	internal      *minio.Client
	presign       *minio.Client
	publicPrefix  string
	BucketMaster  string
	BucketClient  string
	BucketConsult string
}

// New creates internal and offline-presigning MinIO clients.
func New(cfg config.Config) (*Client, error) {
	internal, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIORootUser, cfg.MinIORootPassword, ""),
		Secure: cfg.MinIOUseSSL,
		Region: "us-east-1",
	})
	if err != nil {
		return nil, fmt.Errorf("minio internal client: %w", err)
	}

	presignClient := internal
	publicPrefix := ""
	if cfg.MinIOPublicEndpoint != "" {
		if u, parseErr := url.Parse(cfg.MinIOPublicEndpoint); parseErr == nil && u.Host != "" {
			publicPrefix = strings.TrimRight(u.Path, "/")
			if p, pErr := minio.New(u.Host, &minio.Options{
				Creds:  credentials.NewStaticV4(cfg.MinIORootUser, cfg.MinIORootPassword, ""),
				Secure: strings.EqualFold(u.Scheme, "https"),
				Region: "us-east-1",
			}); pErr == nil {
				presignClient = p
			}
		}
	}

	return &Client{
		internal:      internal,
		presign:       presignClient,
		publicPrefix:  publicPrefix,
		BucketMaster:  cfg.MinIOBucketMaster,
		BucketClient:  cfg.MinIOBucketClient,
		BucketConsult: cfg.MinIOBucketConsultations,
	}, nil
}

// EnsureBuckets creates the configured buckets if they do not exist yet.
func (c *Client) EnsureBuckets(ctx context.Context) error {
	for _, b := range []string{c.BucketMaster, c.BucketClient, c.BucketConsult} {
		if b == "" {
			continue
		}
		exists, err := c.internal.BucketExists(ctx, b)
		if err != nil {
			return fmt.Errorf("check bucket %s: %w", b, err)
		}
		if !exists {
			if err := c.internal.MakeBucket(ctx, b, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
				return fmt.Errorf("make bucket %s: %w", b, err)
			}
		}
	}
	return nil
}

// PutBytes uploads an in-memory payload to MinIO.
func (c *Client) PutBytes(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	_, err := c.internal.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// PutStream uploads a reader of known size to MinIO.
func (c *Client) PutStream(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.internal.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// GetBytes downloads an object from MinIO.
func (c *Client) GetBytes(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := c.internal.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// PresignedGetURL issues a time-limited download URL routed through the public Nginx /s3 proxy.
func (c *Client) PresignedGetURL(ctx context.Context, bucket, key string, ttl time.Duration, downloadFilename string) (string, error) {
	reqParams := make(url.Values)
	if downloadFilename != "" {
		reqParams.Set("response-content-disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(downloadFilename, `"`, "")))
	}
	u, err := c.presign.PresignedGetObject(ctx, bucket, key, ttl, reqParams)
	if err != nil {
		return "", err
	}
	if c.publicPrefix != "" {
		u.Path = c.publicPrefix + u.Path
	}
	return u.String(), nil
}
