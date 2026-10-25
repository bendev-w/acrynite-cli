package wal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"acrynite-cli/internal/location"
)

// BuildArchiveCommand builds the PostgreSQL archive_command executed by the
// database server. The CLI must therefore be installed on the database host.
func BuildArchiveCommand(executable, workingDir, dataDirectory, scheduleID string) string {
	sourcePath := filepath.Join(dataDirectory, "%p")
	if runtime.GOOS == "windows" {
		quote := func(value string) string {
			return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
		}
		return fmt.Sprintf("cd /d %s && %s backup wal push %s --file %s --name %s",
			quote(workingDir), quote(executable), quote(scheduleID), quote(sourcePath), quote("%f"))
	}

	quote := func(value string) string {
		return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
	}
	return fmt.Sprintf("cd %s && %s backup wal push %s --file %s --name %s",
		quote(workingDir), quote(executable), quote(scheduleID), quote(sourcePath), quote("%f"))
}

// ArchiveFile stores one PostgreSQL WAL file in the configured backup location.
func ArchiveFile(cfg *location.Config, sourcePath, archiveName string) (location.BackupRef, error) {
	if archiveName == "" || filepath.Base(archiveName) != archiveName || archiveName == "." {
		return location.BackupRef{}, fmt.Errorf("invalid WAL archive name %q", archiveName)
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return location.BackupRef{}, fmt.Errorf("cannot read WAL file: %w", err)
	}

	if cfg.Type == location.TypeLocal {
		archiveDir := filepath.Join(cfg.Path, "wal")
		if err := os.MkdirAll(archiveDir, 0755); err != nil {
			return location.BackupRef{}, fmt.Errorf("failed to create WAL archive directory: %w", err)
		}
		destination := filepath.Join(archiveDir, archiveName)
		if err := copyArchiveFile(sourcePath, destination); err != nil {
			return location.BackupRef{}, err
		}
		return location.BackupRef{Name: archiveName, Identifier: destination}, nil
	}

	cloudCfg := *cfg
	if cloudCfg.Type == location.TypeS3 {
		cloudCfg.Prefix = strings.Trim(cfg.Prefix, "/")
		if cloudCfg.Prefix != "" {
			cloudCfg.Prefix += "/"
		}
		cloudCfg.Prefix += "wal"
	}
	return cloudCfg.UploadBackup(sourcePath, cfg.Database)
}

func copyArchiveFile(sourcePath, destination string) error {
	if sourceInfo, err := os.Stat(sourcePath); err == nil {
		if destinationInfo, statErr := os.Stat(destination); statErr == nil {
			if sourceInfo.Size() == destinationInfo.Size() {
				return nil
			}
			return fmt.Errorf("WAL archive already exists with a different size: %s", destination)
		}
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open WAL file: %w", err)
	}
	defer source.Close()

	temporary := destination + ".tmp"
	target, err := os.Create(temporary)
	if err != nil {
		return fmt.Errorf("failed to create WAL archive: %w", err)
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("failed to copy WAL archive: %w", err)
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("failed to close WAL archive: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("failed to finalize WAL archive: %w", err)
	}
	return nil
}
