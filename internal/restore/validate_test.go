package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBackupFile_NonExistentFile(t *testing.T) {
	res, err := ValidateBackupFile("non_existent_file.sql", "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsValid {
		t.Errorf("expected IsValid to be false for non-existent file")
	}

	if len(res.Errors) == 0 {
		t.Errorf("expected error messages in res.Errors")
	}
}

func TestValidateBackupFile_EmptyFile(t *testing.T) {
	tempDir := t.TempDir()
	emptyFile := filepath.Join(tempDir, "empty.sql")

	err := os.WriteFile(emptyFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("failed to create temp empty file: %v", err)
	}

	res, err := ValidateBackupFile(emptyFile, "postgres")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsValid {
		t.Errorf("expected IsValid to be false for empty file")
	}

	if res.SizeBytes != 0 {
		t.Errorf("expected size 0, got %d", res.SizeBytes)
	}
}

func TestValidateBackupFile_PostgresValidDump(t *testing.T) {
	tempDir := t.TempDir()
	pgFile := filepath.Join(tempDir, "postgres_backup.sql")

	pgContent := `--
-- PostgreSQL database dump
--

SET statement_timeout = 0;
SET lock_timeout = 0;

CREATE TABLE users (
    id integer NOT NULL,
    username character varying(255) NOT NULL,
    email character varying(255) NOT NULL
);

CREATE TABLE orders (
    id integer NOT NULL,
    user_id integer NOT NULL,
    total numeric(10,2) NOT NULL
);

INSERT INTO users VALUES (1, 'john_doe', 'john@example.com');
`

	err := os.WriteFile(pgFile, []byte(pgContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test dump: %v", err)
	}

	res, err := ValidateBackupFile(pgFile, "postgres")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsValid {
		t.Errorf("expected IsValid to be true, got errors: %v", res.Errors)
	}

	if res.DetectedType != "postgres" {
		t.Errorf("expected DetectedType 'postgres', got '%s'", res.DetectedType)
	}

	if res.TableCount != 2 {
		t.Errorf("expected TableCount 2, got %d", res.TableCount)
	}

	if len(res.TablesFound) != 2 || res.TablesFound[0] != "users" || res.TablesFound[1] != "orders" {
		t.Errorf("expected tables [users orders], got %v", res.TablesFound)
	}

	report := FormatValidationReport(res)
	if !strings.Contains(report, "VALID") || !strings.Contains(report, "users") {
		t.Errorf("validation report missing expected keywords: %s", report)
	}
}

func TestValidateBackupFile_MySQLValidDump(t *testing.T) {
	tempDir := t.TempDir()
	mysqlFile := filepath.Join(tempDir, "mysql_backup.sql")

	mysqlContent := `-- MySQL dump 10.13  Distrib 8.0.32
-- Host: localhost    Database: testdb

SET FOREIGN_KEY_CHECKS=0;

CREATE TABLE ` + "`products`" + ` (
  ` + "`id`" + ` int NOT NULL AUTO_INCREMENT,
  ` + "`name`" + ` varchar(255) NOT NULL,
  PRIMARY KEY (` + "`id`" + `)
) ENGINE=InnoDB;

INSERT INTO ` + "`products`" + ` VALUES (1,'Widget');
`

	err := os.WriteFile(mysqlFile, []byte(mysqlContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test dump: %v", err)
	}

	res, err := ValidateBackupFile(mysqlFile, "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsValid {
		t.Errorf("expected IsValid to be true, got errors: %v", res.Errors)
	}

	if res.DetectedType != "mysql" {
		t.Errorf("expected DetectedType 'mysql', got '%s'", res.DetectedType)
	}

	if res.TableCount != 1 || res.TablesFound[0] != "products" {
		t.Errorf("expected table 'products', got %v", res.TablesFound)
	}
}

func TestValidateBackupFile_TypeMismatchWarning(t *testing.T) {
	tempDir := t.TempDir()
	pgFile := filepath.Join(tempDir, "pg.sql")
	pgContent := `-- PostgreSQL database dump
CREATE TABLE test_table (id int);
`
	_ = os.WriteFile(pgFile, []byte(pgContent), 0644)

	res, err := ValidateBackupFile(pgFile, "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Warnings) == 0 {
		t.Errorf("expected warning for type mismatch (detected postgres, expected mysql)")
	}
}
