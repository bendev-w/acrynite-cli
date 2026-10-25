package wal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acrynite-cli/internal/location"
)

func TestBuildSpoolArchiveCommand(t *testing.T) {
	got := BuildSpoolArchiveCommand("linux", "/wal_archive/")
	want := "cp %p '/wal_archive'/%f.tmp && mv -f '/wal_archive'/%f.tmp '/wal_archive'/%f"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	win := BuildSpoolArchiveCommand("windows", "D:/wal")
	if win != `copy "%p" "D:\wal\%f.tmp" && move /Y "D:\wal\%f.tmp" "D:\wal\%f"` {
		t.Fatalf("unexpected windows command: %s", win)
	}
}

func TestServerOSFromVersion(t *testing.T) {
	cases := map[string]string{
		"PostgreSQL 15.18 on x86_64-pc-linux-musl, compiled by gcc (Alpine 15.2.0)": "linux",
		"PostgreSQL 16.1, compiled by Visual C++ build 1937, 64-bit":                "windows",
		"PostgreSQL 15.4 on aarch64-apple-darwin23.0.0":                              "darwin",
		"PostgreSQL 15 something":                                                    "unknown",
	}
	for in, want := range cases {
		if got := serverOSFromVersion(in); got != want {
			t.Errorf("%q: got %s, want %s", in, got, want)
		}
	}
}

func TestUploadSpoolMovesFinishedFilesOnly(t *testing.T) {
	spool := t.TempDir()
	dest := t.TempDir()
	for name, body := range map[string]string{
		"000000010000000000000001":     "wal1",
		"000000010000000000000002":     "wal2",
		"000000010000000000000003.tmp": "partial",
	} {
		if err := os.WriteFile(filepath.Join(spool, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &location.Config{Name: "l", Database: "db", Type: location.TypeLocal, Path: dest}

	uploaded, failed := UploadSpool(cfg, spool)
	if uploaded != 2 || failed != 0 {
		t.Fatalf("uploaded=%d failed=%d", uploaded, failed)
	}
	if _, err := os.Stat(filepath.Join(dest, "wal", "000000010000000000000002")); err != nil {
		t.Fatalf("segment not archived: %v", err)
	}
	entries, _ := os.ReadDir(spool)
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".tmp") {
		t.Fatalf("spool should only keep the partial file, got %v", entries)
	}
}
