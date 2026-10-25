package location

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// DownloadPrefix materializes every S3 object under the location prefix into
// destDir. It is used only for physical recovery, where PostgreSQL needs the
// complete WAL archive locally for restore_command to consume. Object keys are
// reduced to base names so an object can never escape destDir.
func (cfg *Config) DownloadPrefix(destDir string) ([]string, error) {
	if cfg.Type != TypeS3 {
		return nil, fmt.Errorf("prefix download is only supported for S3 locations")
	}
	client, providerCfg, err := cfg.s3Client()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	var token *string
	var downloaded []string
	for {
		page, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: &providerCfg.Bucket, Prefix: &prefix, ContinuationToken: token})
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", providerCfg.Bucket, prefix, err)
		}
		for _, object := range page.Contents {
			if object.Key == nil || strings.HasSuffix(*object.Key, "/") {
				continue
			}
			name := filepath.Base(*object.Key)
			if name == "." || name == "" {
				continue
			}
			out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: &providerCfg.Bucket, Key: object.Key})
			if err != nil {
				return nil, fmt.Errorf("download s3://%s/%s: %w", providerCfg.Bucket, *object.Key, err)
			}
			file, err := os.Create(filepath.Join(destDir, name))
			if err == nil {
				_, err = io.Copy(file, out.Body)
			}
			closeErr := out.Body.Close()
			if file != nil {
				if fileErr := file.Close(); err == nil {
					err = fileErr
				}
			}
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return nil, err
			}
			downloaded = append(downloaded, name)
		}
		if page.IsTruncated == nil || !*page.IsTruncated {
			break
		}
		token = page.NextContinuationToken
	}
	return downloaded, nil
}
