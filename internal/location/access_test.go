package location

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/smithy-go"
)

func TestCheckWriteAccessLocalLeavesNoProbe(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Name: "l", Type: TypeLocal, Path: dir}
	for _, sub := range []string{"", "wal"} {
		if err := cfg.CheckWriteAccess(sub); err != nil {
			t.Fatalf("CheckWriteAccess(%q): %v", sub, err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "wal"))
	if len(entries) != 0 {
		t.Errorf("probe file left behind: %v", entries)
	}
}

func TestCheckWriteAccessLocalFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Name: "l", Type: TypeLocal, Path: filepath.Join(blocker, "sub")}
	err := cfg.CheckWriteAccess("")
	var accessErr *AccessError
	if !errors.As(err, &accessErr) {
		t.Fatalf("expected *AccessError, got %v", err)
	}
}

func TestCheckWriteAccessUnsupportedType(t *testing.T) {
	cfg := &Config{Name: "g", Type: TypeGDrive}
	if err := cfg.CheckWriteAccess(""); !errors.Is(err, ErrWriteCheckUnsupported) {
		t.Fatalf("expected ErrWriteCheckUnsupported, got %v", err)
	}
}

func TestClassifyS3AccessError(t *testing.T) {
	tests := []struct {
		code   string
		denied bool
	}{
		{"AccessDenied", true},
		{"InvalidAccessKeyId", true},
		{"SignatureDoesNotMatch", true},
		{"NoSuchBucket", false},
		{"PermanentRedirect", false},
	}
	for _, tt := range tests {
		err := classifyS3AccessError("s3://b/", "s3:PutObject", &smithy.GenericAPIError{Code: tt.code, Message: "m"})
		var accessErr *AccessError
		if !errors.As(err, &accessErr) {
			t.Fatalf("%s: expected *AccessError", tt.code)
		}
		if accessErr.Denied != tt.denied {
			t.Errorf("%s: Denied = %v, want %v", tt.code, accessErr.Denied, tt.denied)
		}
		if accessErr.Hint == "" {
			t.Errorf("%s: expected a hint", tt.code)
		}
	}
}
