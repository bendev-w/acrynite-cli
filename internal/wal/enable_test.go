package wal

import "testing"

func TestArchiveConfigurationStatementsEnableAndEscape(t *testing.T) {
	statements := archiveConfigurationStatements("off", `acrynite --label 'nightly'`)
	if len(statements) != 2 {
		t.Fatalf("expected archive command and mode statements, got %d", len(statements))
	}
	if statements[0] != `ALTER SYSTEM SET archive_command = 'acrynite --label ''nightly'''` {
		t.Fatalf("unexpected archive_command statement: %s", statements[0])
	}
	if statements[1] != "ALTER SYSTEM SET archive_mode = 'on'" {
		t.Fatalf("unexpected archive_mode statement: %s", statements[1])
	}
}

func TestArchiveConfigurationStatementsKeepsEnabledMode(t *testing.T) {
	statements := archiveConfigurationStatements("on", "archive command")
	if len(statements) != 1 {
		t.Fatalf("expected only archive_command statement, got %d", len(statements))
	}
}
