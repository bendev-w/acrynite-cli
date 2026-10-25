package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/internal/schedule"
	"acrynite-cli/internal/wal"
	"acrynite-cli/utils"

	"github.com/spf13/cobra"
)

var (
	walSourceFile  string
	walArchiveName string
	// archive_timeout forces a WAL segment switch so idle databases still archive regularly
	walArchiveTimeout string
	walSpoolDir       string
	walContainerDir   string
	walMode           string
)

var backupWALCmd = &cobra.Command{
	Use:   "wal",
	Short: "Manage PostgreSQL WAL archiving",
}

var backupWALEnableCmd = &cobra.Command{
	Use:   "enable [schedule-id]",
	Short: "Enable WAL archiving for a PostgreSQL backup schedule",
	Long: `Configures PostgreSQL archive_mode and archive_command so completed WAL
segments are stored in the local or cloud location linked to the schedule.
The CLI and its storage configuration must remain available on the PostgreSQL host.`,
	Args: cobra.ExactArgs(1),
	RunE: runEnableWALArchive,
}

var backupWALPushCmd = &cobra.Command{
	Use:    "push [schedule-id]",
	Short:  "Store one PostgreSQL WAL segment",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runPushWALArchive,
}

func init() {
	legacyBackupCmd.AddCommand(backupWALCmd)
	backupWALCmd.AddCommand(backupWALEnableCmd)
	backupWALCmd.AddCommand(backupWALPushCmd)

	backupWALEnableCmd.Flags().StringVar(&walMode, "wal-mode", "auto", "How WAL is archived: auto, stream (pg_receivewal over the replication connection; works for any OS/container/remote server), direct (archive_command runs this CLI on the database host) or spool (shared folder)")
	backupWALEnableCmd.Flags().StringVar(&walSpoolDir, "wal-spool-dir", "", "Host folder shared with a containerised PostgreSQL; WAL is copied there by PostgreSQL and uploaded by Acrynite")
	backupWALEnableCmd.Flags().StringVar(&walContainerDir, "wal-container-dir", wal.DefaultContainerSpoolDir, "Path of the shared folder as seen inside the PostgreSQL container")
	backupWALEnableCmd.Flags().StringVar(&walArchiveTimeout, "archive-timeout", "5min", "Force a WAL segment switch at least this often (e.g. 30s, 5min, 1h; 0 disables)")
	backupWALPushCmd.Flags().StringVar(&walSourceFile, "file", "", "PostgreSQL WAL source path")
	backupWALPushCmd.Flags().StringVar(&walArchiveName, "name", "", "PostgreSQL WAL archive filename")
	_ = backupWALPushCmd.MarkFlagRequired("file")
	_ = backupWALPushCmd.MarkFlagRequired("name")
}

func runEnableWALArchive(cmd *cobra.Command, args []string) error {
	scheduleID := args[0]
	scheduleData, target, restartRequired, err := enableWALArchiving(scheduleID)
	if err != nil {
		return err
	}

	utils.Success("WAL archiving enabled for schedule '%s'", scheduleID)
	utils.Info("Database: %s", scheduleData.Database)
	utils.Info("Location: %s (%s)", target.Name, target.Type)
	if restartRequired {
		utils.Warning("Restart PostgreSQL to activate archive_mode; archive_command has already been saved")
	} else {
		utils.Info("PostgreSQL configuration reloaded; no restart is required")
	}
	return nil
}

// enableWALArchiving points PostgreSQL's archive_command at this CLI for the schedule's
// location. Permission problems are reported with the exact privilege that is missing.
func enableWALArchiving(scheduleID string) (*schedule.ScheduleData, *location.Config, bool, error) {
	scheduleData, target, err := resolveWALSchedule(scheduleID)
	if err != nil {
		return nil, nil, false, err
	}

	databaseManager, err := database.GetConfigManager()
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to get database manager: %w", err)
	}
	databaseConfig, err := databaseManager.Get(scheduleData.Database)
	if err != nil {
		return nil, nil, false, err
	}
	if databaseConfig.Type != "postgres" {
		return nil, nil, false, fmt.Errorf("WAL archiving requires PostgreSQL; schedule '%s' uses %s", scheduleID, databaseConfig.Type)
	}

	db, err := database.ConnectPostgresDB(databaseConfig.ConnectionString())
	if err != nil {
		return nil, nil, false, err
	}
	defer db.Close()

	server, err := wal.DetectServer(db)
	if err != nil {
		return nil, nil, false, err
	}
	mode, err := wal.ResolveMode(walMode, walSpoolDir != "", server)
	if err != nil {
		return nil, nil, false, err
	}
	if server.Mode == wal.ModeSpool {
		utils.Info("Detected PostgreSQL on %s: %s", server.OS, server.Reason)
	}
	utils.Info("WAL archive mode: %s", mode)

	if mode == wal.ModeStream {
		slot := wal.SlotName(scheduleID)
		dir, dirErr := wal.StreamDir(scheduleID)
		if dirErr != nil {
			return nil, nil, false, dirErr
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, nil, false, fmt.Errorf("cannot create WAL stream directory %s: %w", dir, err)
		}
		if err := wal.EnableStreaming(db, slot, walArchiveTimeout); err != nil {
			return nil, nil, false, err
		}
		if err := getOrCreateScheduleManager().SetWALSpool(scheduleID, wal.ModeStream, slot, dir, ""); err != nil {
			return nil, nil, false, fmt.Errorf("failed to save WAL stream settings: %w", err)
		}
		utils.Success("Replication slot '%s' ready; the scheduler daemon streams WAL with pg_receivewal and uploads finished segments", slot)
		return scheduleData, target, false, nil
	}

	var archiveCommand string
	if mode == wal.ModeSpool {
		spool := walSpoolDir
		if spool == "" {
			spool = wal.DefaultSpoolDir()
		}
		hostDir, absErr := filepath.Abs(spool)
		if absErr != nil {
			return nil, nil, false, fmt.Errorf("invalid --wal-spool-dir: %w", absErr)
		}
		containerDir := walContainerDir
		if containerDir == "" {
			containerDir = wal.DefaultContainerSpoolDir
		}
		if err := os.MkdirAll(hostDir, 0755); err != nil {
			return nil, nil, false, fmt.Errorf("cannot create WAL spool directory %s: %w", hostDir, err)
		}
		if err := getOrCreateScheduleManager().SetWALSpool(scheduleID, wal.ModeSpool, "", hostDir, containerDir); err != nil {
			return nil, nil, false, fmt.Errorf("failed to save WAL spool settings: %w", err)
		}
		scheduleData.WALSpoolDir, scheduleData.WALContainerDir = hostDir, containerDir
		archiveCommand = wal.BuildSpoolArchiveCommand(server.OS, containerDir)
	} else {
		if err := getOrCreateScheduleManager().SetWALSpool(scheduleID, wal.ModeDirect, "", "", ""); err != nil {
			return nil, nil, false, fmt.Errorf("failed to save WAL settings: %w", err)
		}
		var dataDirectory string
		if err := db.QueryRow("SHOW data_directory").Scan(&dataDirectory); err != nil {
			return nil, nil, false, fmt.Errorf("failed to read PostgreSQL data_directory (requires superuser or pg_read_all_settings): %w", err)
		}
		executable, err := os.Executable()
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to resolve CLI executable: %w", err)
		}
		workingDirectory, err := os.Getwd()
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to resolve CLI working directory: %w", err)
		}
		archiveCommand = wal.BuildArchiveCommand(executable, workingDirectory, dataDirectory, scheduleID)
	}

	restartRequired, err := wal.EnableArchive(db, archiveCommand, walArchiveTimeout)
	if err != nil {
		return nil, nil, false, err
	}
	return scheduleData, target, restartRequired, nil
}

func runPushWALArchive(cmd *cobra.Command, args []string) error {
	_, target, err := resolveWALSchedule(args[0])
	if err != nil {
		return err
	}
	ref, err := wal.ArchiveFile(target, walSourceFile, walArchiveName)
	if err != nil {
		return err
	}
	utils.Debug("Archived WAL segment '%s' to %s", walArchiveName, ref.Identifier)
	return nil
}

func resolveWALSchedule(scheduleID string) (*schedule.ScheduleData, *location.Config, error) {
	schedules, err := getOrCreateScheduleManager().GetSchedules()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load schedules: %w", err)
	}
	scheduled, exists := schedules[scheduleID]
	if !exists {
		return nil, nil, fmt.Errorf("schedule '%s' not found", scheduleID)
	}

	locationManager, err := location.GetConfigManager()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get location manager: %w", err)
	}
	for _, candidate := range locationManager.ListByDatabase(scheduled.Database) {
		if candidate.ScheduleID == scheduleID {
			if !candidate.Active {
				return nil, nil, fmt.Errorf("location '%s' for schedule '%s' is inactive", candidate.Name, scheduleID)
			}
			return scheduled, candidate, nil
		}
	}
	return nil, nil, fmt.Errorf("no backup location is linked to schedule '%s'", scheduleID)
}
