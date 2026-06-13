package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"acrynite-cli/internal/database"
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

func init() {
	// Add subcommands
	backupCmd.AddCommand(backupRunCmd)
	backupCmd.AddCommand(backupScheduleCmd)
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCancelCmd)

	// Backup run flags
	backupRunCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path to store backup files")

	// Backup schedule flags
	backupScheduleCmd.Flags().StringVar(&backupInterval, "interval", "daily", "Backup interval (hourly, daily, weekly, or custom duration like 6h, 30m)")
	backupScheduleCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path to store backup files")
}

func runBackup(cmd *cobra.Command, args []string) error {
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

	// Check if database is active
	if !config.IsActive {
		utils.Warning("Database '%s' is not active. Testing connection first...", dbName)
		if err := manager.TestConnection(dbName); err != nil {
			return fmt.Errorf("connection test failed: %w", err)
		}
		utils.Success("Connection successful!")
		fmt.Println()
	}

	// Ensure backup directory exists
	if err := utils.EnsureDirExists(backupPath); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Generate backup filename with timestamp
	timestamp := utils.Now(utils.TimestampBackup)
	backupFile := filepath.Join(backupPath, fmt.Sprintf("%s_%s.sql", dbName, timestamp))

	utils.Info("Starting backup of database '%s'...", dbName)
	utils.Info("Backup location: %s", backupFile)

	// Perform the backup based on database type
	if config.Type == "mysql" {
		err = performMySqlDump(config, backupFile)
	} else {
		err = performPgDump(config, backupFile)
	}
	
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Get file size
	fileSize, err := utils.GetFileSize(backupFile)
	if err != nil {
		utils.Warning("Could not get backup file size: %v", err)
	} else {
		utils.Success("Backup completed successfully!")
		utils.Info("Backup file: %s", backupFile)
		utils.Info("Backup size: %.2f MB", float64(fileSize)/(1024*1024))
	}

	return nil
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
	interval, err := parseInterval(backupInterval)
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
			err = performMySqlDump(config, backupFile)
		} else {
			err = performPgDump(config, backupFile)
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

	// Start scheduler if not running
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
	scheduler := getOrCreateScheduler()
	tasks := scheduler.ListTasks()

	// Filter backup tasks
	var backupTasks []*utils.Task
	for _, task := range tasks {
		if len(task.ID) > 7 && task.ID[:7] == "backup-" {
			backupTasks = append(backupTasks, task)
		}
	}

	if len(backupTasks) == 0 {
		utils.Info("No scheduled backups")
		utils.Info("Schedule a backup with: acrynite backup schedule <database-name> --interval <interval> --path <path>")
		return nil
	}

	utils.Info("Scheduled Backups (%d):", len(backupTasks))
	fmt.Println()

	for _, task := range backupTasks {
		fmt.Printf("  Schedule ID: %s\n", task.ID)
		fmt.Printf("  Name:        %s\n", task.Name)
		fmt.Printf("  Description: %s\n", task.Description)
		fmt.Printf("  Status:      %s\n", task.Status)
		fmt.Printf("  Interval:    %s\n", task.Schedule)
		
		if !task.NextRun.IsZero() && task.Status == utils.TaskPending {
			fmt.Printf("  Next run:    %s (%s)\n", 
				utils.FormatTimestamp(task.NextRun, utils.TimestampLog),
				utils.TimeAgo(task.NextRun))
		}
		
		fmt.Printf("  Run count:   %d\n", task.RunCount)
		fmt.Printf("  Errors:      %d\n", task.ErrorCount)
		
		if !task.LastRun.IsZero() {
			fmt.Printf("  Last run:    %s (%s)\n",
				utils.FormatTimestamp(task.LastRun, utils.TimestampLog),
				utils.TimeAgo(task.LastRun))
		}
		
		fmt.Println()
	}

	return nil
}

func runCancelBackup(cmd *cobra.Command, args []string) error {
	scheduleID := args[0]

	scheduler := getOrCreateScheduler()
	
	if err := scheduler.RemoveTask(scheduleID); err != nil {
		return fmt.Errorf("failed to cancel backup: %w", err)
	}

	utils.Success("Backup schedule '%s' cancelled successfully!", scheduleID)
	return nil
}

// performPgDump performs a PostgreSQL dump using pg_dump command
func performPgDump(config *database.DBConfig, outputFile string) error {
	// Set environment variable for password
	env := os.Environ()
	env = append(env, fmt.Sprintf("PGPASSWORD=%s", config.Password))

	// Build pg_dump command
	args := []string{
		"-h", config.Host,
		"-p", fmt.Sprintf("%d", config.Port),
		"-U", config.User,
		"-d", config.Database,
		"-F", "p", // Plain text format
		"-f", outputFile,
	}

	// Create command
	cmd := exec.Command("pg_dump", args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute backup
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w (ensure pg_dump is installed and in PATH)", err)
	}

	return nil
}

// performMySqlDump performs a MySQL dump using mysqldump command
func performMySqlDump(config *database.DBConfig, outputFile string) error {
	// Build mysqldump command
	args := []string{
		"-h", config.Host,
		"-P", fmt.Sprintf("%d", config.Port),
		"-u", config.User,
		fmt.Sprintf("-p%s", config.Password), // MySQL requires no space between -p and password
		config.Database,
		"--result-file=" + outputFile,
	}

	// Create command
	cmd := exec.Command("mysqldump", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute backup
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysqldump failed: %w (ensure mysqldump is installed and in PATH)", err)
	}

	return nil
}

// parseInterval converts interval string to time.Duration
func parseInterval(interval string) (time.Duration, error) {
	switch interval {
	case "hourly":
		return time.Hour, nil
	case "daily":
		return 24 * time.Hour, nil
	case "weekly":
		return 7 * 24 * time.Hour, nil
	default:
		// Try parsing as duration
		duration, err := time.ParseDuration(interval)
		if err != nil {
			return 0, fmt.Errorf("invalid interval '%s' (use: hourly, daily, weekly, or duration like 30m, 6h, 24h)", interval)
		}
		if duration < time.Minute {
			return 0, fmt.Errorf("interval must be at least 1 minute")
		}
		return duration, nil
	}
}

// Global scheduler instance
var globalScheduler *utils.Scheduler

// getOrCreateScheduler returns the global scheduler instance
func getOrCreateScheduler() *utils.Scheduler {
	if globalScheduler == nil {
		globalScheduler = utils.NewScheduler(10 * time.Second) // Check every 10 seconds
	}
	return globalScheduler
}
