package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/database"
	"acrynite-cli/internal/schedule"
	"acrynite-cli/utils"

	"github.com/spf13/cobra"
)

var (
	backupPath     string
	backupInterval string
	scheduleID     string
)

// backupCmd represents the backup command
var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage database backups",
	Long:  `Create and schedule PostgreSQL database backups using pg_dump.`,
}

// backupRunCmd performs a full database backup
var backupRunCmd = &cobra.Command{
	Use:   "run [database-name]",
	Short: "Run a full database backup",
	Long: `Performs a full backup of the specified database using pg_dump.
The backup file will be stored at the specified path with a timestamp.`,
	Example: `  acrynite backup run production-db --path ./backups
  acrynite backup run dev-db --path /var/backups/databases`,
	Args: cobra.ExactArgs(1),
	RunE: runBackup,
}

// backupScheduleCmd schedules automated backups
var backupScheduleCmd = &cobra.Command{
	Use:   "schedule [database-name]",
	Short: "Schedule automated database backups",
	Long: `Schedules recurring backups for a database at specified intervals.
Supported intervals: hourly, daily, weekly, or custom duration (e.g., 30m, 2h, 24h).`,
	Example: `  acrynite backup schedule production-db --interval daily --path ./backups
  acrynite backup schedule dev-db --interval 6h --path /var/backups
  acrynite backup schedule test-db --interval weekly --path ./backups`,
	Args: cobra.ExactArgs(1),
	RunE: runScheduleBackup,
}

// backupListCmd lists scheduled backups
var backupListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List scheduled backups",
	Aliases: []string{"ls"},
	RunE:    runListBackups,
}

// backupCancelCmd cancels a scheduled backup
var backupCancelCmd = &cobra.Command{
	Use:   "cancel [schedule-id]",
	Short: "Cancel a scheduled backup",
	Args:  cobra.ExactArgs(1),
	RunE:  runCancelBackup,
}

// backupSchedulerCmd runs the backup scheduler daemon
var backupSchedulerCmd = &cobra.Command{
	Use:    "scheduler",
	Short:  "Run backup scheduler daemon (runs continuously in background)",
	Hidden: true, // Hidden from help - started automatically
	RunE:   runSchedulerDaemon,
}

func init() {
	// Add subcommands
	backupCmd.AddCommand(backupRunCmd)
	backupCmd.AddCommand(backupScheduleCmd)
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCancelCmd)
	backupCmd.AddCommand(backupSchedulerCmd)

	// Backup run flags
	backupRunCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path to store backup files")

	// Backup schedule flags
	backupScheduleCmd.Flags().StringVar(&backupInterval, "interval", "daily", "Backup interval (hourly, daily, weekly, or custom duration like 6h, 30m)")
	backupScheduleCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path to store backup files")
}

func runBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]
	return backup.PerformBackup(dbName, backupPath)
}

func runScheduleBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]

	// Get database configuration
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	config, err := manager.Get(dbName)
	if err != nil {
		return fmt.Errorf("database '%s' not found: %w", dbName, err)
	}

	// Test connection first
	utils.Info("Testing connection to '%s'...", dbName)
	if err := manager.TestConnection(dbName); err != nil {
		return fmt.Errorf("connection test failed - cannot schedule backups: %w", err)
	}
	utils.Success("Connection successful!")
	fmt.Println()

	// Ensure backup directory exists
	if err := utils.EnsureDirExists(backupPath); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Parse interval
	interval, err := backup.ParseInterval(backupInterval)
	if err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	// Create scheduler if not exists
	scheduler := getOrCreateScheduler()

	// Create schedule ID
	scheduleID := fmt.Sprintf("backup-%s-%s", dbName, utils.Now(utils.TimestampFile))

	// Create backup task
	backupTask := func() error {
		timestamp := utils.Now(utils.TimestampBackup)
		backupFile := filepath.Join(backupPath, fmt.Sprintf("%s_%s.sql", dbName, timestamp))

		utils.Info("[Scheduled] Starting backup of '%s'...", dbName)

		var err error
		if config.Type == "mysql" {
			err = backup.PerformMySQLDump(config, backupFile)
		} else {
			err = backup.PerformPostgresDump(config, backupFile)
		}

		if err != nil {
			utils.Error("[Scheduled] Backup failed: %v", err)
			return err
		}

		fileSize, _ := utils.GetFileSize(backupFile)
		utils.Success("[Scheduled] Backup completed: %s (%.2f MB)", backupFile, float64(fileSize)/(1024*1024))
		return nil
	}

	// Schedule the task
	task := utils.NewTask(scheduleID, fmt.Sprintf("Backup %s", dbName), backupTask)
	task.Description = fmt.Sprintf("Automated backup for database '%s' every %s", dbName, backupInterval)
	task.SetSchedule(interval, true)

	if err := scheduler.AddTask(task); err != nil {
		return fmt.Errorf("failed to schedule backup: %w", err)
	}

	// Save schedule to persistence
	scheduleManager := getOrCreateScheduleManager()
	if err := scheduleManager.AddSchedule(scheduleID, dbName, interval, backupPath); err != nil {
		utils.Warning("Failed to persist schedule: %v", err)
		// Don't fail the entire operation if persistence fails, but warn the user
	}

	// Start daemon if not already running
	if !scheduleManager.IsDaemonRunning() {
		utils.Info("Starting backup scheduler daemon...")
		if err := scheduleManager.StartDaemon(); err != nil {
			utils.Warning("Failed to start daemon: %v", err)
		} else {
			utils.Success("Scheduler daemon started successfully!")
		}
	} else {
		utils.Info("Scheduler daemon is already running")
	}

	// Start scheduler if not running (for in-memory scheduling if needed)
	scheduler.Start()

	utils.Success("Backup scheduled successfully!")
	utils.Info("Database: %s", dbName)
	utils.Info("Interval: %s (%s)", backupInterval, interval)
	utils.Info("Output path: %s", backupPath)
	utils.Info("Schedule ID: %s", scheduleID)
	utils.Info("Next backup: %s", utils.FormatTimestamp(task.NextRun, utils.TimestampLog))

	fmt.Println()
	utils.Info("Note: Scheduled backups run while the CLI is active.")
	utils.Info("To cancel this schedule, run: acrynite backup cancel %s", scheduleID)

	return nil
}

func runListBackups(cmd *cobra.Command, args []string) error {
	manager := getOrCreateScheduleManager()

	// Execute any overdue schedules first
	if err := manager.ExecuteOverdueSchedules(); err != nil {
		utils.Warning("Failed to execute overdue schedules: %v", err)
	}

	schedules, err := manager.GetSchedules()
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	if len(schedules) == 0 {
		utils.Info("No scheduled backups")
		utils.Info("Schedule a backup with: acrynite backup schedule <database-name> --interval <interval> --path <path>")
		return nil
	}

	utils.Info("Scheduled Backups (%d):", len(schedules))
	fmt.Println()

	for _, scheduleData := range schedules {
		fmt.Printf("  Schedule ID: %s\n", scheduleData.ID)
		fmt.Printf("  Database:    %s\n", scheduleData.Database)
		fmt.Printf("  Interval:    %s\n", scheduleData.Interval)
		fmt.Printf("  Output path: %s\n", scheduleData.Path)
		fmt.Printf("  Created:     %s\n", utils.FormatTimestamp(scheduleData.CreatedAt, utils.TimestampLog))
		fmt.Printf("  Run count:   %d\n", scheduleData.RunCount)
		fmt.Printf("  Errors:      %d\n", scheduleData.ErrorCount)

		if !scheduleData.LastRunTime.IsZero() {
			fmt.Printf("  Last run:    %s (%s)\n",
				utils.FormatTimestamp(scheduleData.LastRunTime, utils.TimestampLog),
				utils.TimeAgo(scheduleData.LastRunTime))
		} else {
			fmt.Printf("  Last run:    Never\n")
		}

		fmt.Println()
	}

	return nil
}

func runCancelBackup(cmd *cobra.Command, args []string) error {
	scheduleID := args[0]

	scheduler := getOrCreateScheduler()

	// Try to remove from in-memory scheduler (if running)
	_ = scheduler.RemoveTask(scheduleID)

	// Remove from persistence
	manager := getOrCreateScheduleManager()
	if err := manager.RemoveSchedule(scheduleID); err != nil {
		return fmt.Errorf("failed to cancel backup: %w", err)
	}

	utils.Success("Backup schedule '%s' cancelled successfully!", scheduleID)
	return nil
}

func runSchedulerDaemon(cmd *cobra.Command, args []string) error {
	manager := getOrCreateScheduleManager()
	checkInterval := 30 * time.Second // Check every 30 seconds for overdue schedules

	utils.Info("Backup scheduler daemon started")
	utils.Info("Checking for scheduled backups every %v", checkInterval)
	fmt.Println()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		// Execute any overdue schedules
		if err := manager.ExecuteOverdueSchedules(); err != nil {
			utils.Error("Error executing overdue schedules: %v", err)
		}
	}

	return nil
}

// Global scheduler instance
var globalScheduler *utils.Scheduler

// Global schedule manager
var globalScheduleManager *schedule.Manager

// getOrCreateScheduler returns the global scheduler instance
func getOrCreateScheduler() *utils.Scheduler {
	if globalScheduler == nil {
		globalScheduler = utils.NewScheduler(10 * time.Second) // Check every 10 seconds
	}
	return globalScheduler
}

// getOrCreateScheduleManager returns the global schedule manager
func getOrCreateScheduleManager() *schedule.Manager {
	if globalScheduleManager == nil {
		storageDir := filepath.Join("storage", "schedules")
		globalScheduleManager = schedule.NewManager(storageDir)
	}
	return globalScheduleManager
}
