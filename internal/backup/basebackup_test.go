package backup

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyBaseBackupError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"pg_hba", `pg_basebackup: error: connection to server failed: FATAL:  no pg_hba.conf entry for replication connection from host "10.0.0.5", user "bk"`, "pg_hba.conf has no replication entry"},
		{"replication role", "pg_basebackup: error: could not start WAL sender: FATAL:  must be superuser or replication role to start walsender", "lacks the REPLICATION attribute"},
		{"password", `pg_basebackup: error: connection failed: FATAL:  password authentication failed for user "bk"`, "authentication failed"},
		{"wal senders", "pg_basebackup: error: FATAL:  number of requested standby connections exceeds max_wal_senders (currently 1)", "WAL sender slots"},
		{"wal level", "ERROR:  WAL level not sufficient for making an online backup", "wal_level is too low"},
		{"filesystem", "pg_basebackup: error: could not create directory: Permission denied", "permission denied while running"},
		{"unknown", "something odd", "exit status 1"},
	}
	for _, tt := range tests {
		err := classifyBaseBackupError(tt.stderr, errors.New("exit status 1"))
		var bbErr *BaseBackupError
		if !errors.As(err, &bbErr) {
			t.Fatalf("%s: expected *BaseBackupError, got %T", tt.name, err)
		}
		if !strings.Contains(bbErr.Reason, tt.want) {
			t.Errorf("%s: reason %q does not contain %q", tt.name, bbErr.Reason, tt.want)
		}
	}
}

func TestBundleDirectoryPacksFlatFiles(t *testing.T) {
	src := t.TempDir()
	for name, content := range map[string]string{"base.tar.gz": "base", "pg_wal.tar.gz": "wal"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "db_basebackup_20260101-000000.tar")
	if err := bundleDirectory(src, dest); err != nil {
		t.Fatalf("bundleDirectory: %v", err)
	}
	if _, err := os.Stat(dest + ".partial"); err == nil {
		t.Error("partial file was not cleaned up")
	}

	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := map[string]string{}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		got[hdr.Name] = string(body)
	}
	if got["base.tar.gz"] != "base" || got["pg_wal.tar.gz"] != "wal" || len(got) != 2 {
		t.Errorf("unexpected archive contents: %v", got)
	}
}

func TestBundleDirectoryRejectsEmptyOutput(t *testing.T) {
	if err := bundleDirectory(t.TempDir(), filepath.Join(t.TempDir(), "x.tar")); err == nil {
		t.Fatal("expected error for empty pg_basebackup output")
	}
}

func TestResolveBaseBackupExecutableFindsSiblingOfDumpPath(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "pg_dump")
	sibling := filepath.Join(dir, "pg_basebackup")
	for _, p := range []string{dump, sibling} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ResolveBaseBackupExecutable(dump)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sibling {
		t.Errorf("expected %s, got %s", sibling, got)
	}
}

func TestResolveBaseBackupExecutableMissingSibling(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "pg_dump")
	if err := os.WriteFile(dump, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBaseBackupExecutable(dump); err == nil {
		t.Fatal("expected error when pg_basebackup is not next to the configured dump path")
	}
}
