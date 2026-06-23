package aws

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DigitalTolk/keel/internal/backup"
)

// TestS3DestinationRoundTrip runs against any S3-compatible endpoint (real AWS,
// Backblaze B2, or MinIO). Set KEEL_S3_TEST_ENDPOINT (and optionally
// KEEL_S3_TEST_KEY / _SECRET) to enable it; otherwise it is skipped.
func TestS3DestinationRoundTrip(t *testing.T) {
	endpoint := os.Getenv("KEEL_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set KEEL_S3_TEST_ENDPOINT to run the S3 integration test")
	}
	key := envOr("KEEL_S3_TEST_KEY", "minioadmin")
	secret := envOr("KEEL_S3_TEST_SECRET", "minioadmin")

	ctx := context.Background()
	d, err := NewS3Destination(ctx, S3Options{
		Bucket:          "keel-test",
		Region:          "us-east-1",
		Endpoint:        endpoint,
		AccessKeyID:     key,
		SecretAccessKey: secret,
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("NewS3Destination: %v", err)
	}
	if err := d.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	// Confirm it satisfies the Destination contract.
	var _ backup.Destination = d

	src := filepath.Join(t.TempDir(), "f.tar.gz")
	if err := os.WriteFile(src, []byte("hello-s3"), 0o600); err != nil {
		t.Fatal(err)
	}

	prefix := "it/"
	for _, k := range []string{"it/a.tar.gz", "it/b.tar.gz", "it/c.tar.gz"} {
		if err := d.Put(ctx, k, src); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	objs, err := d.List(ctx, prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("List = %d objects, want 3", len(objs))
	}

	// Purge keep=1 should leave exactly one object.
	deleted, err := backup.Purge(ctx, d, prefix, 1)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("Purge deleted %d, want 2", len(deleted))
	}
	remaining, _ := d.List(ctx, prefix)
	if len(remaining) != 1 {
		t.Fatalf("after purge: %d objects remain, want 1", len(remaining))
	}

	// Clean up.
	for _, o := range remaining {
		_ = d.Delete(ctx, o.Key)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
