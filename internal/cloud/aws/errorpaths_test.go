package aws

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestS3ErrorPaths points the client at an unreachable endpoint with retries
// disabled, so every S3 call fails fast — exercising the error-wrapping
// branches of Put (including the SSE-KMS path), List, Delete, and EnsureBucket
// without needing a live backend.
func TestS3ErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d, err := NewS3Destination(ctx, S3Options{
		Bucket:          "b",
		Region:          "us-east-1",
		Endpoint:        "http://127.0.0.1:1",
		AccessKeyID:     "k",
		SecretAccessKey: "s",
		KMSKeyID:        "arn:aws:kms:::key/abc", // forces the SSE-KMS branch in Put
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	// Disable retries so failures return immediately.
	d.client = s3.NewFromConfig(aws.Config{Region: "us-east-1", RetryMaxAttempts: 1}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("http://127.0.0.1:1")
		o.UsePathStyle = true
		o.Credentials = staticCreds{}
	})

	src := filepath.Join(t.TempDir(), "o.tar.gz")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := d.Put(ctx, "k", src); err == nil {
		t.Error("Put should fail against a dead endpoint")
	}
	if _, err := d.List(ctx, "p/"); err == nil {
		t.Error("List should fail against a dead endpoint")
	}
	if err := d.Delete(ctx, "k"); err == nil {
		t.Error("Delete should fail against a dead endpoint")
	}
	if err := d.EnsureBucket(ctx); err == nil {
		t.Error("EnsureBucket should fail against a dead endpoint")
	}
}

type staticCreds struct{}

func (staticCreds) Retrieve(context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "k", SecretAccessKey: "s"}, nil
}
