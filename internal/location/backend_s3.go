package location

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"acrynite-cli/internal/provider"
	"acrynite-cli/utils"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3Client resolves the provider referenced by this location into a ready S3 client.
func (cfg *Config) s3Client() (*s3.Client, *provider.Config, error) {
	if cfg.Provider == "" {
		return nil, nil, fmt.Errorf("location '%s' has no provider configured", cfg.Name)
	}

	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get provider manager: %w", err)
	}
	pCfg, err := providerMgr.Get(cfg.Provider)
	if err != nil {
		return nil, nil, err
	}

	secrets, err := provider.NewSecretStore()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize secret store: %w", err)
	}

	client, err := provider.NewS3Client(pCfg, secrets)
	if err != nil {
		return nil, nil, err
	}
	return client, pCfg, nil
}

// s3KeyPrefix returns the key prefix backups for this database are stored under,
// derived from the location's configured prefix (or the database name by default).
func (cfg *Config) s3KeyPrefix(dbName string) string {
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix == "" {
		return dbName + "_"
	}
	return prefix + "/" + dbName + "_"
}

// s3ObjectKey returns the full object key a given local backup filename should be stored under.
func (cfg *Config) s3ObjectKey(filename string) string {
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix == "" {
		return filename
	}
	return prefix + "/" + filename
}

func (cfg *Config) listS3Backups(dbName string) ([]BackupRef, error) {
	client, pCfg, err := cfg.s3Client()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := cfg.s3KeyPrefix(dbName)
	var refs []BackupRef
	var continuationToken *string
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &pCfg.Bucket,
			Prefix:            &prefix,
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list objects in bucket '%s': %w", pCfg.Bucket, err)
		}
		for _, obj := range out.Contents {
			if obj.Key != nil {
				name := baseName(*obj.Key)
				if strings.HasSuffix(name, ".sql") && !validBackupName(dbName, name) {
					utils.Warning("Skipping malformed S3 backup filename: %s", name)
					continue
				}
				if validBackupName(dbName, name) {
					refs = append(refs, BackupRef{Name: name, Identifier: *obj.Key})
				}
			}
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

func (cfg *Config) uploadS3Backup(localPath, dbName string) (BackupRef, error) {
	client, pCfg, err := cfg.s3Client()
	if err != nil {
		return BackupRef{}, err
	}

	filename := baseName(localPath)
	key := cfg.s3ObjectKey(filename)

	file, err := os.Open(localPath)
	if err != nil {
		return BackupRef{}, fmt.Errorf("failed to open backup file for upload: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &pCfg.Bucket,
		Key:    &key,
		Body:   file,
	}); err != nil {
		return BackupRef{}, fmt.Errorf("failed to upload backup to s3://%s/%s: %w", pCfg.Bucket, key, err)
	}

	return BackupRef{Name: filename, Identifier: key}, nil
}

func (cfg *Config) downloadS3Backup(key, destPath string) error {
	client, pCfg, err := cfg.s3Client()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &pCfg.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to download s3://%s/%s: %w", pCfg.Bucket, key, err)
	}
	defer out.Body.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create local file for download: %w", err)
	}
	defer dest.Close()

	if _, err := io.Copy(dest, out.Body); err != nil {
		return fmt.Errorf("failed to write downloaded backup: %w", err)
	}
	return nil
}
