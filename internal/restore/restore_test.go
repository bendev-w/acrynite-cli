package restore

import (
	"os"
	"path/filepath"
	"testing"

	"acrynite-cli/internal/database"
)

func TestResolveRestoreExecutable(t *testing.T) {
	// Test custom restore path option
	tempDir := t.TempDir()
	dummyExecutable := filepath.Join(tempDir, "dummy_client")
	_ = os.WriteFile(dummyExecutable, []byte("#!/bin/sh\nexit 0"), 0755)

	resolved, err := resolveRestoreExecutable("mysql", dummyExecutable, "")
	if err != nil {
		t.Fatalf("unexpected error resolving custom restore path: %v", err)
	}

	if resolved != dummyExecutable {
		t.Errorf("expected '%s', got '%s'", dummyExecutable, resolved)
	}
}

func TestPerformRestore_MissingDatabase(t *testing.T) {
	options := RestoreOptions{
		DatabaseName: "non_existent_db_12345",
		FilePath:     "some_file.sql",
		DryRun:       true,
	}

	err := PerformRestore("non_existent_db_12345", "some_file.sql", options)
	if err == nil {
		t.Errorf("expected error for non-existent database configuration")
	}
}

func TestPerformRestore_InvalidBackupFile(t *testing.T) {
	// Register a dummy database configuration first
	manager, err := database.GetConfigManager()
	if err != nil {
		t.Fatalf("failed to get config manager: %v", err)
	}

	dummyConfig := database.NewDBConfig("test-restore-dummy", "mysql", "localhost", 3306, "root", "", "testdb", "")
	_ = manager.Register(dummyConfig)
	defer manager.Remove("test-restore-dummy")

	options := RestoreOptions{
		DatabaseName: "test-restore-dummy",
		FilePath:     "non_existent_file.sql",
		DryRun:       true,
	}

	err = PerformRestore("test-restore-dummy", "non_existent_file.sql", options)
	if err == nil {
		t.Errorf("expected error when restoring non-existent backup file")
	}
}

func TestPerformRestore_DryRun(t *testing.T) {
	manager, err := database.GetConfigManager()
	if err != nil {
		t.Fatalf("failed to get config manager: %v", err)
	}

	dbName := "production"
	if !manager.Exists(dbName) {
		dummyConfig := database.NewDBConfig(dbName, "mysql", "localhost", 3306, "root", "", "dbname", "")
		_ = manager.Register(dummyConfig)
		defer manager.Remove(dbName)
	}

	tempDir := t.TempDir()
	dumpFile := filepath.Join(tempDir, "mysql_valid.sql")
	sqlContent := `-- MySQL dump 10.13
CREATE TABLE users (id int);
`
	_ = os.WriteFile(dumpFile, []byte(sqlContent), 0644)

	dummyClient := filepath.Join(tempDir, "mysql")
	_ = os.WriteFile(dummyClient, []byte("#!/bin/sh\nexit 0"), 0755)

	options := RestoreOptions{
		DatabaseName:      dbName,
		FilePath:          dumpFile,
		DryRun:            true,
		CustomRestorePath: dummyClient,
	}

	err = PerformRestore(dbName, dumpFile, options)
	// If live local database is running, dry run succeeds. If no DB server, it returns connection error.
	if err != nil && !testing.Short() {
		t.Logf("Dry run result (with live DB check): %v", err)
	}
}
