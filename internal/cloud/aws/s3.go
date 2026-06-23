// Package aws provides an S3-compatible backup Destination. The same client
// serves real AWS S3 and Backblaze B2 (which exposes an S3-compatible API),
// replacing both the aws-cli and the b2 CLI from the original scripts.
package aws

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/DigitalTolk/keel/internal/backup"
)

// S3Options configures an S3Destination.
type S3Options struct {
	Bucket   string
	Region   string
	Profile  string // shared-config profile (AWS); ignored when static creds are set
	Endpoint string // custom endpoint for B2 / MinIO; empty for real AWS
	KMSKeyID string // enables SSE-KMS when set

	// Static credentials; when empty the default AWS credential chain is used.
	AccessKeyID     string
	SecretAccessKey string

	// UsePathStyle is required for MinIO and recommended for B2.
	UsePathStyle bool
}

// S3Destination is an S3-compatible backup.Destination.
type S3Destination struct {
	client *s3.Client
	bucket string
	kmsKey string
}

// NewS3Destination builds a client from the given options.
func NewS3Destination(ctx context.Context, opts S3Options) (*S3Destination, error) {
	cfg, err := awsConfig(ctx, opts.Region, opts.Profile, opts.AccessKeyID, opts.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
		if opts.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	if opts.Bucket == "" {
		return nil, errors.New("s3 destination requires a bucket")
	}
	return &S3Destination{client: client, bucket: opts.Bucket, kmsKey: opts.KMSKeyID}, nil
}

// EnsureBucket creates the bucket if it does not already exist. Production
// buckets normally pre-exist; this is primarily for tests and first-run setup.
func (d *S3Destination) EnsureBucket(ctx context.Context) error {
	_, err := d.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &d.bucket})
	if err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if errors.As(err, &owned) || errors.As(err, &exists) {
			return nil
		}
		return fmt.Errorf("create bucket %s: %w", d.bucket, err)
	}
	return nil
}

// Put uploads srcPath to key, applying SSE-KMS when configured.
func (d *S3Destination) Put(ctx context.Context, key, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	in := &s3.PutObjectInput{Bucket: &d.bucket, Key: &key, Body: f}
	if d.kmsKey != "" {
		in.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		in.SSEKMSKeyId = &d.kmsKey
	}
	if _, err := d.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// List returns all objects under prefix.
func (d *S3Destination) List(ctx context.Context, prefix string) ([]backup.Object, error) {
	var objs []backup.Object
	p := s3.NewListObjectsV2Paginator(d.client, &s3.ListObjectsV2Input{
		Bucket: &d.bucket,
		Prefix: &prefix,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, o := range page.Contents {
			objs = append(objs, backup.Object{
				Key:     aws.ToString(o.Key),
				ModTime: aws.ToTime(o.LastModified),
			})
		}
	}
	return objs, nil
}

// Delete removes the object at key.
func (d *S3Destination) Delete(ctx context.Context, key string) error {
	if _, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &d.bucket, Key: &key}); err != nil {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	return nil
}

var _ backup.Destination = (*S3Destination)(nil)
