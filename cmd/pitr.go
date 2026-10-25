package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/internal/pitr"
	"acrynite-cli/internal/wal"
	"acrynite-cli/utils"

	"github.com/spf13/cobra"
)

var preflightForSchedule bool

var backupPreflightCmd = &cobra.Command{
	Use:   "preflight [database-name]",
	Short: "Check permissions needed for pg_basebackup and WAL archiving",
	Long: `Verifies everything point-in-time recovery depends on, and reports each missing
permission with the exact fix:

  - pg_basebackup is installed, executable and not older than the server
  - the database role has the REPLICATION attribute and pg_hba.conf allows replication
  - wal_level / max_wal_senders allow base backups
  - WAL archiving is configured, the role may configure it, and the archiver is healthy
  - the WAL directory can be inspected
  - the backup destination (local folder or S3 bucket) is writable, including its wal/ path`,
	Example: `  acrynite backup preflight production-db
  acrynite backup preflight production-db --location nightly-s3
  acrynite backup preflight production-db --type s3 --provider work-bucket --for-schedule`,
	Args: cobra.ExactArgs(1),
	RunE: runPreflight,
}

func init() {
	legacyBackupCmd.AddCommand(backupPreflightCmd)
	backupPreflightCmd.Flags().StringVar(&locationName, "location", "", "Registered location to check for write access")
	backupPreflightCmd.Flags().StringVar(&locationType, "type", "local", "Ad-hoc location type to check: local or s3")
	backupPreflightCmd.Flags().StringVar(&locationProviderFlag, "provider", "", "Registered provider name (for --type=s3)")
	backupPreflightCmd.Flags().StringVar(&locationPrefixFlag, "prefix", "", "S3 key prefix")
	backupPreflightCmd.Flags().StringVar(&backupPath, "path", "./backups", "Local backup directory to check")
	backupPreflightCmd.Flags().StringVar(&walMode, "wal-mode", "auto", "WAL archive mode to verify: auto, stream, direct or spool")
	backupPreflightCmd.Flags().StringVar(&walSpoolDir, "wal-spool-dir", "", "Host folder shared with a containerised PostgreSQL (verifies the mount and write access)")
	backupPreflightCmd.Flags().StringVar(&walContainerDir, "wal-container-dir", wal.DefaultContainerSpoolDir, "Path of the shared folder inside the PostgreSQL container")
	backupPreflightCmd.Flags().BoolVar(&preflightForSchedule, "for-schedule", false, "Treat missing WAL-archiving privileges as failures, as when creating a schedule")
}

func validateBackupMethod(method string) error {
	switch method {
	case backup.MethodDump, backup.MethodBaseBackup:
		return nil
	default:
		return fmt.Errorf("unsupported backup method '%s' (expected '%s' or '%s')", method, backup.MethodDump, backup.MethodBaseBackup)
	}
}

func runPreflight(cmd *cobra.Command, args []string) error {
	dbName := args[0]
	dbManager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get database manager: %w", err)
	}
	config, err := dbManager.Get(dbName)
	if err != nil {
		return fmt.Errorf("database '%s' not found: %w", dbName, err)
	}

	opts := pitr.Options{RequireArchive: preflightForSchedule, WALSpoolDir: walSpoolDir, WALContainerDir: walContainerDir, WALMode: walMode}
	switch {
	case locationName != "":
		locMgr, err := location.GetConfigManager()
		if err != nil {
			return fmt.Errorf("failed to get location manager: %w", err)
		}
		loc, err := locMgr.Get(dbName, locationName)
		if err != nil {
			return err
		}
		opts.Location = loc
		opts.StagingDir = stagingDirFor(loc)
	case cmd.Flags().Changed("type") || cmd.Flags().Changed("path") || cmd.Flags().Changed("provider"):
		locType := location.Type(locationType)
		if locType == location.TypeGDrive {
			return fmt.Errorf("preflight cannot probe gdrive locations; use --type local or s3")
		}
		if err := validateLocationProvider(locType, locationProviderFlag); err != nil {
			return err
		}
		loc := &location.Config{Name: "preflight", Database: dbName, Type: locType, Provider: locationProviderFlag, Prefix: locationPrefixFlag}
		if locType == location.TypeLocal {
			loc.Path = backupPath
		}
		opts.Location = loc
		opts.StagingDir = stagingDirFor(loc)
	}

	utils.Info("Running point-in-time recovery pre-flight checks for '%s'...", dbName)
	report := pitr.Run(config, opts)
	report.Log()

	if err := report.Err(); err != nil {
		return err
	}
	utils.Success("Pre-flight checks passed; pg_basebackup and WAL archiving are permitted")
	return nil
}

// stagingDirFor is where base backups are written before upload; local locations are
// written in place.
func stagingDirFor(loc *location.Config) string {
	if loc.Type == location.TypeLocal {
		return loc.Path
	}
	return filepath.Join(os.TempDir(), "acrynite-cli", "backups")
}

// runBaseBackupOnce performs a single pg_basebackup for 'backup run --method basebackup'.
func runBaseBackupOnce(dbName string) error {
	dbManager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get database manager: %w", err)
	}
	config, err := dbManager.Get(dbName)
	if err != nil {
		return fmt.Errorf("database '%s' not found: %w", dbName, err)
	}

	var target *location.Config
	var locMgr *location.ConfigManager
	staging := backupPath
	if locationName != "" {
		locMgr, err = location.GetConfigManager()
		if err != nil {
			return fmt.Errorf("failed to get location manager: %w", err)
		}
		target, err = locMgr.Get(dbName, locationName)
		if err != nil {
			return err
		}
		staging = stagingDirFor(target)
	}

	utils.Info("Running pre-flight checks...")
	report := pitr.Run(config, pitr.Options{Location: target, StagingDir: staging})
	report.Log()
	if err := report.Err(); err != nil {
		return err
	}

	ref, err := backup.PerformBaseBackupToLocation(config, dbName, staging, target)
	if err != nil {
		return err
	}
	if target != nil {
		_ = locMgr.TouchLastBackup(dbName, target.Name, time.Now())
		utils.Success("Base backup stored in %s location '%s': %s", target.Type, target.Name, ref.Name)
	}
	return nil
}
