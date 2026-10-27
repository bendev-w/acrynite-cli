package cmd

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// expandArchivePlaceholders mimics PostgreSQL's handling of %p, %f and %% in archive_command.
func expandArchivePlaceholders(command, path, file string) string {
	var b strings.Builder
	for i := 0; i < len(command); i++ {
		if command[i] == '%' && i+1 < len(command) {
			switch command[i+1] {
			case 'p':
				b.WriteString(path)
				i++
				continue
			case 'f':
				b.WriteString(file)
				i++
				continue
			case '%':
				b.WriteByte('%')
				i++
				continue
			}
		}
		b.WriteByte(command[i])
	}
	return b.String()
}

func TestBuildDatabaseArchiveCommandResolvesFromDataDirectory(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	dataDir, workDir := t.TempDir(), t.TempDir()
	// "echo" stands in for the Acrynite executable so the resolved arguments can be inspected.
	command := buildDatabaseArchiveCommand("echo", workDir, "db")
	command = expandArchivePlaceholders(command, "pg_wal/000000010000000000000004", "000000010000000000000004")

	run := exec.Command(sh, "-c", command)
	run.Dir = dataDir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("archive command failed: %v: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "wal push db --file ") || !strings.HasSuffix(got, " --name 000000010000000000000004") {
		t.Fatalf("unexpected arguments: %q", got)
	}
	file := strings.TrimSuffix(strings.TrimPrefix(got, "wal push db --file "), " --name 000000010000000000000004")
	if filepath.Base(filepath.Dir(file)) != "pg_wal" || !strings.HasSuffix(file, "/pg_wal/000000010000000000000004") || strings.Contains(file, filepath.Base(workDir)) {
		t.Fatalf("--file must point into the data directory, got %q", file)
	}
}

func TestBuildDatabaseArchiveCommandWindows(t *testing.T) {
	command := buildDatabaseArchiveCommand(`C:\acrynite\acrynite.exe`, `C:\work`, "db")
	command = expandArchivePlaceholders(command, `pg_wal\000000010000000000000004`, "000000010000000000000004")
	want := `cd /d "C:\work" && "C:\acrynite\acrynite.exe" wal push "db" --file "%CD%\pg_wal\000000010000000000000004" --name "000000010000000000000004"`
	if command != want {
		t.Fatalf("got  %s\nwant %s", command, want)
	}
}
