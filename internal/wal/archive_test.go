package wal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acrynite-cli/internal/location"
)

func TestBuildArchiveCommandIncludesPostgresPlaceholders(t *testing.T) {
	command := BuildArchiveCommand(filepath.Join("test dir", "acrynite"), filepath.Join("work dir"), filepath.Join("pg", "data"), "backup-db-123")

	for _, expected := range []string{"backup wal push", "backup-db-123", filepath.Join("pg", "data", "%p"), "%f"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("command %q does not contain %q", command, expected)
		}
	}
}

func TestArchiveFileStoresLocalWALIdempotently(t *testing.T) {
	source := filepath.Join(t.TempDir(), "000000010000000000000001")
	if err := os.WriteFile(source, []byte("wal-data"), 0600); err != nil {
		t.Fatal(err)
	}
	destinationRoot := t.TempDir()
	cfg := &location.Config{Type: location.TypeLocal, Path: destinationRoot}

	ref, err := ArchiveFile(cfg, source, filepath.Base(source))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Identifier != filepath.Join(destinationRoot, "wal", filepath.Base(source)) {
		t.Fatalf("unexpected archive path %q", ref.Identifier)
	}
	if _, err := ArchiveFile(cfg, source, filepath.Base(source)); err != nil {
		t.Fatalf("repeated archive should succeed: %v", err)
	}
}

func TestArchiveFileRejectsTraversal(t *testing.T) {
	cfg := &location.Config{Type: location.TypeLocal, Path: t.TempDir()}
	if _, err := ArchiveFile(cfg, "missing", filepath.Join("..", "outside")); err == nil {
		t.Fatal("expected traversal archive name to be rejected")
	}
}
