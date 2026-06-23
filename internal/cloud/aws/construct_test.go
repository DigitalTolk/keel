package aws

import (
	"context"
	"testing"
)

func TestNewS3DestinationRequiresBucket(t *testing.T) {
	if _, err := NewS3Destination(context.Background(), S3Options{Region: "us-east-1"}); err == nil {
		t.Fatal("expected error when bucket is empty")
	}
}

func TestNewS3DestinationConstructs(t *testing.T) {
	d, err := NewS3Destination(context.Background(), S3Options{
		Bucket:          "b",
		Region:          "us-east-1",
		Endpoint:        "https://s3.example",
		AccessKeyID:     "k",
		SecretAccessKey: "s",
		Profile:         "",
		KMSKeyID:        "arn:kms",
		UsePathStyle:    true,
	})
	if err != nil || d == nil {
		t.Fatalf("construct: %v", err)
	}
}
