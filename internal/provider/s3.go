package provider

import (
	"context"
	"fmt"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// NewS3Client builds an S3 client for a registered provider using the access/secret
// keys held in the secret store. Supports S3-compatible endpoints via cfg.Endpoint.
func NewS3Client(cfg *Config, secrets *SecretStore) (*s3.Client, error) {
	if cfg.Type != TypeS3 {
		return nil, fmt.Errorf("provider '%s' is not an s3 provider", cfg.Name)
	}

	accessKey, err := secrets.Get(cfg.Name + ":access_key")
	if err != nil {
		return nil, fmt.Errorf("failed to read access key for provider '%s': %w", cfg.Name, err)
	}
	secretKey, err := secrets.Get(cfg.Name + ":secret_key")
	if err != nil {
		return nil, fmt.Errorf("failed to read secret key for provider '%s': %w", cfg.Name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
			o.UsePathStyle = true // most S3-compatible services need path-style addressing
		}
	}), nil
}

// VerifyS3Bucket confirms the credentials are valid and the bucket is reachable.
func VerifyS3Bucket(cfg *Config, secrets *SecretStore) error {
	client, err := NewS3Client(cfg, secrets)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &cfg.Bucket})
	if err != nil {
		return fmt.Errorf("bucket '%s' is not reachable with the given credentials: %w", cfg.Bucket, err)
	}
	return nil
}
