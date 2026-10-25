package backup

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/utils"
)

// BaseBackupInfix marks physical base backup files so they are never confused with
// pg_dump files ("<db>_<timestamp>.sql") when listing a location.
const BaseBackupInfix = "_basebackup_"

// Backup methods understood by the scheduler.
const (
	MethodDump       = "dump"
	MethodBaseBackup = "basebackup"
)

// BaseBackupError is returned when pg_basebackup fails. Reason and Hint translate the
// raw server/client message into something actionable, especially for permission problems.
type BaseBackupError struct {
	Reason string
	Hint   string
	Stderr string
	Err    error
}

func (e *BaseBackupError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "pg_basebackup failed: %s", e.Reason)
	if e.Hint != "" {
		fmt.Fprintf(&b, " (%s)", e.Hint)
	}
	if e.Stderr != "" {
		fmt.Fprintf(&b, "; pg_basebackup said: %s", e.Stderr)
	}
	return b.String()
}

func (e *BaseBackupError) Unwrap() error { return e.Err }

// ResolveBaseBackupExecutable locates pg_basebackup using the same lookup rules as pg_dump
// (database dump_path sibling, ACRYNITE_PG_BASEBACKUP_PATH / PG_BASEBACKUP_PATH, PATH, then
// standard install locations).
func ResolveBaseBackupExecutable(customDumpPath string) (string, error) {
	return resolveExecutable("pg_basebackup", "postgres", customDumpPath)
}

// ResolveReceiveWALExecutable locates pg_receivewal (shipped next to pg_basebackup) with the same rules.
func ResolveReceiveWALExecutable(customDumpPath string) (string, error) {
	return resolveExecutable("pg_receivewal", "postgres", customDumpPath)
}

// PerformBaseBackup takes a physical base backup of the whole PostgreSQL cluster with
// pg_basebackup and packs the result into a single "<db>_basebackup_<timestamp>.tar" file
// inside outputDir. Combined with WAL archiving it enables point-in-time recovery.
func PerformBaseBackup(config *database.DBConfig, dbName, outputDir string) (string, error) {
	if config.Type != "postgres" {
		return "", fmt.Errorf("pg_basebackup is only supported for PostgreSQL databases ('%s' is %s)", dbName, config.Type)
	}

	exe, err := ResolveBaseBackupExecutable(config.DumpPath)
	if err != nil {
		return "", err
	}
	if err := utils.EnsureDirExists(outputDir); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	workDir, err := os.MkdirTemp(outputDir, ".basebackup-*")
	if err != nil {
		return "", classifyBaseBackupError("", fmt.Errorf("cannot create working directory in '%s': %w", outputDir, err))
	}
	defer os.RemoveAll(workDir)

	// -X stream makes the backup self-contained by streaming the WAL needed to reach a
	// consistent state; archived WAL then provides everything after it.
	args := []string{
		"-h", config.Host,
		"-p", fmt.Sprintf("%d", config.Port),
		"-U", config.User,
		"-D", workDir,
		"-F", "t",
		"-z",
		"-X", "stream",
		"--checkpoint=fast",
		"-w", // never prompt: scheduled runs have no terminal
	}

	env := append(os.Environ(), "PGPASSWORD="+config.Password, "PGCONNECT_TIMEOUT=30")
	if config.SSLMode != "" {
		env = append(env, "PGSSLMODE="+config.SSLMode)
	}

	utils.Info("Starting pg_basebackup of cluster hosting '%s' (%s:%d)...", dbName, config.Host, config.Port)
	utils.Info("pg_basebackup executable: %s", exe)
	utils.Info("pg_basebackup working directory: %s", workDir)

	var stderr bytes.Buffer
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", classifyBaseBackupError(stderr.String(), err)
	}
	utils.Success("pg_basebackup finished; packaging backup files")

	timestamp := utils.Now(utils.TimestampBackup)
	finalPath := filepath.Join(outputDir, fmt.Sprintf("%s%s%s.tar", dbName, BaseBackupInfix, timestamp))
	if err := bundleDirectory(workDir, finalPath); err != nil {
		return "", err
	}

	if size, sizeErr := utils.GetFileSize(finalPath); sizeErr == nil {
		utils.Info("Base backup size: %.2f MB", float64(size)/(1024*1024))
	}
	utils.Success("Base backup created: %s", finalPath)
	return finalPath, nil
}

// PerformBaseBackupToLocation takes a base backup in stagingPath and, for remote locations,
// uploads it and removes the staged copy. For local locations stagingPath is the final
// destination and nothing is moved.
func PerformBaseBackupToLocation(config *database.DBConfig, dbName, stagingPath string, target *location.Config) (location.BackupRef, error) {
	file, err := PerformBaseBackup(config, dbName, stagingPath)
	if err != nil {
		return location.BackupRef{}, err
	}
	if target == nil || target.Type == location.TypeLocal {
		return location.BackupRef{Name: filepath.Base(file), Identifier: file}, nil
	}

	utils.Info("Uploading base backup to %s location '%s'...", target.Type, target.Name)
	ref, err := target.UploadBackup(file, dbName)
	if err != nil {
		if rmErr := os.Remove(file); rmErr != nil {
			utils.Warning("Failed to delete staged base backup '%s' after upload failure: %v", file, rmErr)
		}
		return location.BackupRef{}, err
	}
	if rmErr := os.Remove(file); rmErr != nil {
		utils.Warning("Failed to delete staged base backup '%s': %v", file, rmErr)
	} else {
		utils.Info("Deleted staged base backup: %s", file)
	}
	return ref, nil
}

// bundleDirectory packs the regular files of dir (flat) into one uncompressed tar; the
// members written by pg_basebackup (base.tar.gz, pg_wal.tar.gz, ...) are already gzipped.
func bundleDirectory(dir, destination string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read base backup output: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("pg_basebackup produced no files in %s", dir)
	}

	partial := destination + ".partial"
	out, err := os.Create(partial)
	if err != nil {
		return classifyBaseBackupError("", fmt.Errorf("cannot create '%s': %w", partial, err))
	}
	tw := tar.NewWriter(out)

	fail := func(err error) error {
		tw.Close()
		out.Close()
		_ = os.Remove(partial)
		return err
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fail(fmt.Errorf("failed to stat %s: %w", entry.Name(), err))
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fail(err)
		}
		header.Name = entry.Name()
		if err := tw.WriteHeader(header); err != nil {
			return fail(fmt.Errorf("failed to write tar header for %s: %w", entry.Name(), err))
		}
		src, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return fail(fmt.Errorf("failed to open %s: %w", entry.Name(), err))
		}
		_, copyErr := io.Copy(tw, src)
		src.Close()
		if copyErr != nil {
			return fail(fmt.Errorf("failed to add %s to base backup: %w", entry.Name(), copyErr))
		}
	}

	if err := tw.Close(); err != nil {
		out.Close()
		_ = os.Remove(partial)
		return fmt.Errorf("failed to finalize base backup archive: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("failed to close base backup archive: %w", err)
	}
	if err := os.Rename(partial, destination); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("failed to finalize base backup archive: %w", err)
	}
	return nil
}

var fatalLine = regexp.MustCompile(`(?im)^(?:pg_basebackup:\s*)?(?:error|fatal):\s*(.+)$`)

// classifyBaseBackupError turns pg_basebackup's stderr into an actionable error. Permission
// related failures (replication role, pg_hba.conf, file access) are reported explicitly.
func classifyBaseBackupError(stderr string, runErr error) error {
	detail := strings.TrimSpace(stderr)
	if m := fatalLine.FindAllStringSubmatch(detail, -1); len(m) > 0 {
		detail = strings.TrimSpace(m[len(m)-1][1])
	}
	if len(detail) > 500 {
		detail = detail[:500] + "..."
	}
	haystack := strings.ToLower(stderr)
	if runErr != nil {
		haystack += " " + strings.ToLower(runErr.Error())
	}

	e := &BaseBackupError{Stderr: detail, Err: runErr}
	switch {
	case strings.Contains(haystack, "no pg_hba.conf entry"):
		e.Reason = "permission denied: pg_hba.conf has no replication entry for this user and host"
		e.Hint = "add a line such as 'host replication <user> <client-ip>/32 scram-sha-256' to pg_hba.conf, then reload PostgreSQL"
	case strings.Contains(haystack, "must be superuser or replication role"),
		strings.Contains(haystack, "permission denied to start wal sender"),
		strings.Contains(haystack, "must have the replication"):
		e.Reason = "permission denied: the database role lacks the REPLICATION attribute"
		e.Hint = "run 'ALTER ROLE <user> WITH REPLICATION;' as a superuser (on managed services grant the provider's replication role)"
	case strings.Contains(haystack, "password authentication failed"),
		strings.Contains(haystack, "no password supplied"),
		strings.Contains(haystack, "authentication failed"):
		e.Reason = "authentication failed for the replication connection"
		e.Hint = "check the user name and password stored for this database"
	case strings.Contains(haystack, "max_wal_senders"),
		strings.Contains(haystack, "number of requested standby connections"):
		e.Reason = "the server has no free WAL sender slots"
		e.Hint = "set max_wal_senders to at least 2 (pg_basebackup streams WAL on a second connection) and restart PostgreSQL"
	case strings.Contains(haystack, "wal level not sufficient"),
		strings.Contains(haystack, "wal_level"):
		e.Reason = "wal_level is too low for base backups"
		e.Hint = "set wal_level = replica (or logical) and restart PostgreSQL"
	case strings.Contains(haystack, "incompatible server version"),
		strings.Contains(haystack, "server version"):
		e.Reason = "pg_basebackup is older than the PostgreSQL server"
		e.Hint = "install a pg_basebackup of the same or newer major version, or set ACRYNITE_PG_BASEBACKUP_PATH"
	case strings.Contains(haystack, "application control policy"),
		strings.Contains(haystack, "access is denied"),
		strings.Contains(haystack, "permission denied"):
		e.Reason = "permission denied while running pg_basebackup or writing its output"
		e.Hint = "make sure this user can execute pg_basebackup and write to the staging/backup directory"
	case strings.Contains(haystack, "no space left"),
		strings.Contains(haystack, "not enough space"):
		e.Reason = "the backup directory is out of disk space"
		e.Hint = "free space or point the schedule at a larger volume"
	case strings.Contains(haystack, "could not connect"),
		strings.Contains(haystack, "connection refused"),
		strings.Contains(haystack, "timeout expired"),
		strings.Contains(haystack, "no such host"):
		e.Reason = "could not connect to the PostgreSQL server"
		e.Hint = "check host, port and firewall rules"
	default:
		e.Reason = "unexpected error"
		if runErr != nil {
			e.Reason = runErr.Error()
		}
	}
	return e
}
