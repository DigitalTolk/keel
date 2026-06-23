package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// awsConfig loads an AWS config from the given region/profile, optionally with
// static credentials. Shared by the S3 and EC2 (security-group) clients.
func awsConfig(ctx context.Context, region, profile, accessKeyID, secretAccessKey string) (aws.Config, error) {
	var loadOpts []func(*config.LoadOptions) error
	if region != "" {
		loadOpts = append(loadOpts, config.WithRegion(region))
	}
	if profile != "" {
		loadOpts = append(loadOpts, config.WithSharedConfigProfile(profile))
	}
	if accessKeyID != "" {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		))
	}
	return config.LoadDefaultConfig(ctx, loadOpts...)
}
