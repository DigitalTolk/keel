package aws

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestS3ExtraPaths covers EnsureBucket idempotency, the Put error path, and
// Delete against an S3-compatible endpoint (MinIO). Gated like the main
// integration test.
func TestS3ExtraPaths(t *testing.T) {
	endpoint := os.Getenv("KEEL_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set KEEL_S3_TEST_ENDPOINT to run")
	}
	ctx := context.Background()
	d, err := NewS3Destination(ctx, S3Options{
		Bucket:          "keel-cover",
		Region:          "us-east-1",
		Endpoint:        endpoint,
		AccessKeyID:     envOr("KEEL_S3_TEST_KEY", "minioadmin"),
		SecretAccessKey: envOr("KEEL_S3_TEST_SECRET", "minioadmin"),
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("NewS3Destination: %v", err)
	}

	// First create, then a second call must be a no-op (already-owned branch).
	if err := d.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket #1: %v", err)
	}
	if err := d.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket #2 (idempotent): %v", err)
	}

	// Put error: a missing source file fails before any network call.
	if err := d.Put(ctx, "x/missing.tar.gz", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Put of a missing file should error")
	}

	// Put a real object, list it, then delete it.
	src := filepath.Join(t.TempDir(), "o.tar.gz")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(ctx, "x/o.tar.gz", src); err != nil {
		t.Fatalf("Put: %v", err)
	}
	objs, err := d.List(ctx, "x/")
	if err != nil || len(objs) == 0 {
		t.Fatalf("List: objs=%d err=%v", len(objs), err)
	}
	if err := d.Delete(ctx, "x/o.tar.gz"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
