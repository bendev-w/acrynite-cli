package location

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acrynite-cli/utils"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// ErrWriteCheckUnsupported is returned by CheckWriteAccess for backends that cannot be
// probed without side effects (the caller should report "not verified", not a failure).
var ErrWriteCheckUnsupported = errors.New("write access check is not supported for this location type")

// AccessError describes why a location cannot be written to, with a fix hint. Denied is
// true when the cause is a permission problem rather than a missing resource or outage.
type AccessError struct {
	Target string
	Reason string
	Hint   string
	Denied bool
	Err    error
}

func (e *AccessError) Error() string {
	msg := fmt.Sprintf("cannot write to %s: %s", e.Target, e.Reason)
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return msg
}

func (e *AccessError) Unwrap() error { return e.Err }

// CheckWriteAccess proves the location accepts writes by creating and removing a tiny probe
// object. subdir ("" or e.g. "wal") selects the folder/prefix within the location, since WAL
// segments are stored under their own sub-path and IAM/file permissions may differ.
func (cfg *Config) CheckWriteAccess(subdir string) error {
	switch cfg.Type {
	case TypeLocal:
		return cfg.checkLocalWrite(subdir)
	case TypeS3:
		return cfg.checkS3Write(subdir)
	default:
		return ErrWriteCheckUnsupported
	}
}

func (cfg *Config) checkLocalWrite(subdir string) error {
	if cfg.Path == "" {
		return &AccessError{Target: fmt.Sprintf("location '%s'", cfg.Name), Reason: "no local path configured"}
	}
	dir := cfg.Path
	if subdir != "" {
		dir = filepath.Join(dir, subdir)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return localAccessError(dir, "create directory", err)
	}
	probe, err := os.CreateTemp(dir, ".acrynite-write-test-*")
	if err != nil {
		return localAccessError(dir, "create file", err)
	}
	probePath := probe.Name()
	_, writeErr := probe.Write([]byte("ok"))
	closeErr := probe.Close()
	_ = os.Remove(probePath)
	if writeErr != nil {
		return localAccessError(dir, "write file", writeErr)
	}
	if closeErr != nil {
		return localAccessError(dir, "write file", closeErr)
	}
	return nil
}

func localAccessError(dir, op string, err error) error {
	denied := errors.Is(err, os.ErrPermission)
	reason := fmt.Sprintf("failed to %s: %v", op, err)
	hint := ""
	if denied {
		reason = fmt.Sprintf("permission denied to %s", op)
		hint = "grant the user running Acrynite (and the PostgreSQL service account for WAL archiving) write access to this directory"
	}
	return &AccessError{Target: dir, Reason: reason, Hint: hint, Denied: denied, Err: err}
}

func (cfg *Config) checkS3Write(subdir string) error {
	client, pCfg, err := cfg.s3Client()
	if err != nil {
		return &AccessError{Target: fmt.Sprintf("location '%s'", cfg.Name), Reason: err.Error(), Err: err}
	}

	parts := []string{}
	if prefix := strings.Trim(cfg.Prefix, "/"); prefix != "" {
		parts = append(parts, prefix)
	}
	if subdir != "" {
		parts = append(parts, subdir)
	}
	dir := strings.Join(parts, "/")
	parts = append(parts, fmt.Sprintf(".acrynite-write-test-%d", time.Now().UnixNano()))
	key := strings.Join(parts, "/")
	target := fmt.Sprintf("s3://%s/%s", pCfg.Bucket, dir)
	if dir != "" {
		target += "/"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &pCfg.Bucket,
		Key:    &key,
		Body:   bytes.NewReader([]byte("ok")),
	}); err != nil {
		return classifyS3AccessError(target, "s3:PutObject", err)
	}

	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &pCfg.Bucket, Key: &key}); err != nil {
		utils.Warning("Write probe uploaded but could not be deleted (s3:DeleteObject denied?): s3://%s/%s: %v", pCfg.Bucket, key, err)
	}
	return nil
}

func classifyS3AccessError(target, action string, err error) error {
	code := ""
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}

	e := &AccessError{Target: target, Err: err}
	switch code {
	case "AccessDenied", "AllAccessDisabled", "Forbidden":
		e.Denied = true
		e.Reason = fmt.Sprintf("access denied (%s)", action)
		e.Hint = fmt.Sprintf("allow %s on this bucket/prefix in the IAM user or bucket policy", action)
	case "InvalidAccessKeyId", "SignatureDoesNotMatch", "InvalidToken", "ExpiredToken":
		e.Denied = true
		e.Reason = fmt.Sprintf("credentials were rejected (%s)", code)
		e.Hint = "re-enter the access key and secret with 'provider remove' then 'provider add s3'"
	case "NoSuchBucket":
		e.Reason = "bucket does not exist"
		e.Hint = "check the bucket name and region of the provider"
	case "PermanentRedirect", "AuthorizationHeaderMalformed":
		e.Reason = "bucket is in a different region than the provider is configured for"
		e.Hint = "re-register the provider with the bucket's region"
	default:
		e.Reason = err.Error()
	}
	return e
}
