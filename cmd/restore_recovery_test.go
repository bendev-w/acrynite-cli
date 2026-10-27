package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testStartSegment = "000000010000000000000004"

func newRestoredDataDir(t *testing.T, withBundledWAL bool) string {
	t.Helper()
	dir := t.TempDir()
	label := "START WAL LOCATION: 0/4000028 (file " + testStartSegment + ")\nCHECKPOINT LOCATION: 0/4000080\n"
	if err := os.WriteFile(filepath.Join(dir, "backup_label"), []byte(label), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pg_wal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if withBundledWAL {
		if err := os.WriteFile(filepath.Join(dir, "pg_wal", testStartSegment), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Left behind by pg_basebackup -R or an earlier attempt; must not survive.
	if err := os.WriteFile(filepath.Join(dir, "standby.signal"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readAutoConf(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "postgresql.auto.conf"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigureRecoveryBaseOnly(t *testing.T) {
	dir := newRestoredDataDir(t, true)
	if err := configureRecovery(dir, "", false); err != nil {
		t.Fatal(err)
	}
	for _, signal := range []string{"recovery.signal", "standby.signal"} {
		if _, err := os.Stat(filepath.Join(dir, signal)); !os.IsNotExist(err) {
			t.Fatalf("%s must not exist for a base-only restore", signal)
		}
	}
	conf := readAutoConf(t, dir)
	if strings.Contains(conf, "restore_command") || strings.Contains(conf, "recovery_target") {
		t.Fatalf("base-only restore must not configure archive recovery:\n%s", conf)
	}
	if !strings.Contains(conf, "archive_mode = 'off'") {
		t.Fatalf("restored copy must not archive WAL:\n%s", conf)
	}
}

func TestConfigureRecoveryPITR(t *testing.T) {
	dir := newRestoredDataDir(t, true)
	if err := configureRecovery(dir, "2026-10-26T21:00:00Z", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "recovery.signal")); err != nil {
		t.Fatal("PITR requires recovery.signal")
	}
	if _, err := os.Stat(filepath.Join(dir, "standby.signal")); !os.IsNotExist(err) {
		t.Fatal("standby.signal must be removed")
	}
	conf := readAutoConf(t, dir)
	for _, want := range []string{
		"restore_command = 'test -f /wal_archive/%f && cp /wal_archive/%f %p'",
		"recovery_target_time = '2026-10-26T21:00:00Z'",
		"recovery_target_action = 'promote'",
		"archive_mode = 'off'",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("missing %q in:\n%s", want, conf)
		}
	}
}

func TestValidateBundledWAL(t *testing.T) {
	if err := validateBundledWAL(newRestoredDataDir(t, true)); err != nil {
		t.Fatal(err)
	}
	if err := validateBundledWAL(newRestoredDataDir(t, false)); err == nil {
		t.Fatal("expected error when pg_wal lacks the start segment")
	}
}

func TestValidateWALArchive(t *testing.T) {
	dataDir := newRestoredDataDir(t, true)
	write := func(dir string, names ...string) {
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	empty := t.TempDir()
	write(empty, "notes.txt")
	if _, err := validateWALArchive(dataDir, empty, false); err == nil {
		t.Fatal("expected error for an archive without segments")
	}

	stale := t.TempDir()
	write(stale, "000000010000000000000002", "000000010000000000000003")
	if _, err := validateWALArchive(dataDir, stale, true); err == nil {
		t.Fatal("expected error when the archive ends before the backup and a target time is set")
	}
	if warning, err := validateWALArchive(dataDir, stale, false); err != nil || warning == "" {
		t.Fatalf("expected a warning only, got warning=%q err=%v", warning, err)
	}

	good := t.TempDir()
	write(good, "000000010000000000000004", "000000020000000000000006", "00000002.history")
	if warning, err := validateWALArchive(dataDir, good, true); err != nil || warning != "" {
		t.Fatalf("expected a valid archive, got warning=%q err=%v", warning, err)
	}
}
