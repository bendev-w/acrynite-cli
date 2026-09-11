package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"acrynite-cli/internal/backup"
	"acrynite-cli/internal/database"
	"acrynite-cli/internal/location"
	"acrynite-cli/internal/restore"
	"acrynite-cli/internal/schedule"
	"acrynite-cli/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
)

var (
	backupPath        string
	backupInterval    string
	scheduleID        string
	restoreFile       string
	dryRunFlag        bool
	forceRestoreFlag  bool
	customRestorePath string
	expectedTypeFlag  string

	locationName        string
	locationType        string
	locationSource      string
	restoreSchedule     string
	restoreSourceFilter string
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

// backupListCmd lists scheduled backups, or locations for a specific database
var backupListCmd = &cobra.Command{
	Use:     "list [database-name]",
	Short:   "List scheduled backups, or backup locations for a database",
	Aliases: []string{"ls"},
	Args:    cobra.MaximumNArgs(1),
	RunE:    runListBackups,
}

// backupUnscheduleCmd stops a schedule but keeps its location restorable
var backupUnscheduleCmd = &cobra.Command{
	Use:   "unschedule [database-name]",
	Short: "Stop a scheduled backup job, keeping its location restorable",
	Long: `Stops the recurring job for a named location. The location itself and
its existing backups are kept (marked inactive) so it remains restorable.`,
	Example: `  acrynite backup unschedule production-db --name=nightly`,
	Args:    cobra.ExactArgs(1),
	RunE:    runUnscheduleBackup,
}

// backupRegisterCmd manually registers a location without creating a schedule
var backupRegisterCmd = &cobra.Command{
	Use:     "register [database-name]",
	Short:   "Manually register a backup location without a schedule",
	Long:    `Adds a location for a database without creating a recurring job (e.g. for inherited backups or one-off imports).`,
	Example: `  acrynite backup register production-db --name=imported --type=local --path=./old-backups`,
	Args:    cobra.ExactArgs(1),
	RunE:    runRegisterBackup,
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

// backupRestoreCmd restores a database from a backup file
var backupRestoreCmd = &cobra.Command{
	Use:   "restore [database-name]",
	Short: "Restore a database from a backup file",
	Long: `Restores a database from a SQL dump backup file using psql or mysql client.
Performs pre-flight checks on connection, backup file validity, and client tools.`,
	Example: `  acrynite backup restore production-db --file ./backups/production-db_20260907.sql
  acrynite backup restore dev-db --file ./backups/dev_20260907.sql --dry-run
  acrynite backup restore test-db --file ./backups/test.sql --force`,
	Args: cobra.ExactArgs(1),
	RunE: runRestoreBackup,
}

// backupVerifyCmd validates and inspects a backup file
var backupVerifyCmd = &cobra.Command{
	Use:     "verify [file-path]",
	Short:   "Verify and validate a backup file",
	Long:    `Inspects a SQL dump file, checks structure, line count, table names, and verifies file integrity.`,
	Aliases: []string{"validate", "check"},
	Example: `  acrynite backup verify ./backups/production-db_20260907.sql
  acrynite backup validate ./backups/test.sql --type mysql`,
	Args: cobra.MaximumNArgs(1),
	RunE: runVerifyBackup,
}

func init() {
	// Add subcommands
	backupCmd.AddCommand(backupRunCmd)
	backupCmd.AddCommand(backupScheduleCmd)
	backupCmd.AddCommand(backupUnscheduleCmd)
	backupCmd.AddCommand(backupRegisterCmd)
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCancelCmd)
	backupCmd.AddCommand(backupSchedulerCmd)
	backupCmd.AddCommand(backupRestoreCmd)
	backupCmd.AddCommand(backupVerifyCmd)

	// Backup run flags
	backupRunCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path to store backup files")

	// Backup schedule flags
	backupScheduleCmd.Flags().StringVar(&backupInterval, "interval", "daily", "Backup interval (hourly, daily, weekly, or custom duration like 6h, 30m)")
	backupScheduleCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path to store backup files")
	backupScheduleCmd.Flags().StringVar(&locationName, "name", "", "Label for the location this schedule writes to (defaults to an auto-generated name)")
	backupScheduleCmd.Flags().StringVar(&locationType, "type", "local", "Location type: only 'local' is supported until cloud provider SDK integration lands")

	// Backup unschedule flags
	backupUnscheduleCmd.Flags().StringVar(&locationName, "name", "", "Label of the location whose schedule should be stopped (required)")
	_ = backupUnscheduleCmd.MarkFlagRequired("name")

	// Backup register flags
	backupRegisterCmd.Flags().StringVar(&locationName, "name", "", "Label for this location (required)")
	backupRegisterCmd.Flags().StringVar(&locationType, "type", "local", "Location type: only 'local' is supported until cloud provider SDK integration lands")
	backupRegisterCmd.Flags().StringVar(&backupPath, "path", "./backups", "Path where backups for this location are stored")
	_ = backupRegisterCmd.MarkFlagRequired("name")

	// Backup restore flags
	backupRestoreCmd.Flags().StringVarP(&restoreFile, "file", "f", "", "Restore this exact backup file, bypassing location discovery entirely")
	backupRestoreCmd.Flags().StringVar(&restoreSchedule, "schedule", "", "Restore from this named location only, or 'any' to combine all locations (alias: --location)")
	backupRestoreCmd.Flags().StringVar(&restoreSchedule, "location", "", "Alias for --schedule")
	backupRestoreCmd.Flags().StringVar(&restoreSourceFilter, "source", "all", "Filter candidate locations by origin: auto|manual|all")
	backupRestoreCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Test restore pre-conditions (connection & file validation) without applying changes")
	backupRestoreCmd.Flags().BoolVar(&forceRestoreFlag, "force", false, "Skip confirmation prompt")
	backupRestoreCmd.Flags().StringVar(&customRestorePath, "custom-path", "", "Custom path to psql or mysql client executable")

	// Backup verify flags
	backupVerifyCmd.Flags().StringVarP(&restoreFile, "file", "f", "", "Path to SQL backup file")
	backupVerifyCmd.Flags().StringVar(&expectedTypeFlag, "type", "", "Expected database type (postgres or mysql)")
}

func runBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]
	return backup.PerformBackup(dbName, backupPath)
}

func runScheduleBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]

	if location.Type(locationType) != location.TypeLocal {
		return fmt.Errorf("location type '%s' is not supported yet - only 'local' works until cloud provider SDK integration lands", locationType)
	}

	if locationName == "" {
		locationName = fmt.Sprintf("schedule-%s", utils.Now(utils.TimestampFile))
	}

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

	// Auto-create/update the durable location entry this schedule writes to.
	locMgr, err := location.GetConfigManager()
	if err != nil {
		utils.Warning("Failed to get location manager: %v", err)
	} else {
		loc := &location.Config{
			Name:       locationName,
			Database:   dbName,
			Type:       location.TypeLocal,
			Path:       backupPath,
			Source:     location.SourceSchedule,
			ScheduleID: scheduleID,
			Active:     true,
		}
		if err := locMgr.Upsert(loc); err != nil {
			utils.Warning("Failed to save location '%s': %v", locationName, err)
		}
	}

	utils.Success("Backup scheduled successfully!")
	utils.Info("Database: %s", dbName)
	utils.Info("Location: %s", locationName)
	utils.Info("Interval: %s (%s)", backupInterval, interval)
	utils.Info("Output path: %s", backupPath)
	utils.Info("Schedule ID: %s", scheduleID)
	utils.Info("Next backup: %s", utils.FormatTimestamp(task.NextRun, utils.TimestampLog))

	fmt.Println()
	utils.Info("Note: Scheduled backups run while the CLI is active.")
	utils.Info("To cancel this schedule, run: acrynite backup cancel %s", scheduleID)
	utils.Info("To stop it but keep the location restorable, run: acrynite backup unschedule %s --name=%s", dbName, locationName)

	return nil
}

func runUnscheduleBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]

	locMgr, err := location.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get location manager: %w", err)
	}

	loc, err := locMgr.Get(dbName, locationName)
	if err != nil {
		return err
	}

	if loc.ScheduleID != "" {
		scheduler := getOrCreateScheduler()
		_ = scheduler.RemoveTask(loc.ScheduleID)

		scheduleManager := getOrCreateScheduleManager()
		if err := scheduleManager.RemoveSchedule(loc.ScheduleID); err != nil {
			utils.Warning("Failed to remove underlying schedule '%s': %v", loc.ScheduleID, err)
		}
	}

	if err := locMgr.SetActive(dbName, locationName, false); err != nil {
		return fmt.Errorf("failed to mark location inactive: %w", err)
	}

	utils.Success("Location '%s' unscheduled for database '%s' (kept and still restorable)", locationName, dbName)
	return nil
}

func runRegisterBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]

	if location.Type(locationType) != location.TypeLocal {
		return fmt.Errorf("location type '%s' is not supported yet - only 'local' works until cloud provider SDK integration lands", locationType)
	}

	locMgr, err := location.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get location manager: %w", err)
	}

	loc := &location.Config{
		Name:     locationName,
		Database: dbName,
		Type:     location.TypeLocal,
		Path:     backupPath,
		Source:   location.SourceManual,
		Active:   true,
	}

	if err := locMgr.Register(loc); err != nil {
		return err
	}

	utils.Success("Location '%s' registered for database '%s' (path: %s)", locationName, dbName, backupPath)
	return nil
}

func runListBackups(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return runListLocations(args[0])
	}

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

func runListLocations(dbName string) error {
	locMgr, err := location.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get location manager: %w", err)
	}

	locations := locMgr.ListByDatabase(dbName)
	if len(locations) == 0 {
		utils.Info("No locations registered for database '%s'", dbName)
		utils.Info("Create one with: acrynite backup schedule %s --name=<label> --interval <interval> --path <path>", dbName)
		utils.Info("Or manually: acrynite backup register %s --name=<label> --path <path>", dbName)
		return nil
	}

	utils.Info("Locations for '%s' (%d):", dbName, len(locations))
	fmt.Println()

	for _, loc := range locations {
		status := "inactive"
		if loc.Active {
			status = "active"
		}

		latest, err := loc.FindLatestBackup(dbName)
		latestDisplay := "none"
		if err == nil {
			latestDisplay = latest
		}

		fmt.Printf("  Name:        %s\n", loc.Name)
		fmt.Printf("  Type:        %s\n", loc.Type)
		fmt.Printf("  Source:      %s\n", loc.Source)
		fmt.Printf("  Status:      %s\n", status)
		fmt.Printf("  Path:        %s\n", loc.Path)
		fmt.Printf("  Latest file: %s\n", latestDisplay)
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

func runRestoreBackup(cmd *cobra.Command, args []string) error {
	dbName := args[0]

	if restoreFile == "" {
		resolved, err := resolveRestoreFile(dbName)
		if err != nil {
			return err
		}
		restoreFile = resolved
	}

	// Prompt for confirmation unless --force or --dry-run is set
	if !forceRestoreFlag && !dryRunFlag {
		var confirm bool
		prompt := &survey.Confirm{
			Message: fmt.Sprintf("Restoring '%s' will modify target database '%s'. Are you sure you want to proceed?", restoreFile, dbName),
			Default: false,
		}
		if err := survey.AskOne(prompt, &confirm); err != nil {
			return err
		}
		if !confirm {
			utils.Info("Restore operation cancelled")
			return nil
		}
	}

	options := restore.RestoreOptions{
		DatabaseName:      dbName,
		FilePath:          restoreFile,
		DryRun:            dryRunFlag,
		CustomRestorePath: customRestorePath,
	}

	return restore.PerformRestore(dbName, restoreFile, options)
}

// resolveRestoreFile finds the backup file to restore when --file was not given,
// using the location layer: --schedule/--location picks a specific location (or
// "any" to combine all of them), otherwise it auto-selects when there's exactly
// one candidate location and prompts the user when there's more than one.
func resolveRestoreFile(dbName string) (string, error) {
	locMgr, err := location.GetConfigManager()
	if err != nil {
		return "", fmt.Errorf("failed to get location manager: %w", err)
	}

	candidates := filterLocationsBySource(locMgr.ListByDatabase(dbName), restoreSourceFilter)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no locations registered for database '%s' (use --file to restore an exact backup, or 'acrynite backup register'/'schedule' to add one)", dbName)
	}

	// --schedule=any (or --location=any): combine all candidates, pick the newest overall
	if restoreSchedule == "any" {
		return latestAcrossLocations(candidates, dbName)
	}

	// --schedule=<name> / --location=<name>: restrict to that one location
	if restoreSchedule != "" {
		var selected *location.Config
		for _, loc := range candidates {
			if loc.Name == restoreSchedule {
				selected = loc
				break
			}
		}
		if selected == nil {
			return "", fmt.Errorf("location '%s' not found for database '%s' (use 'acrynite backup list %s' to see available locations)", restoreSchedule, dbName, dbName)
		}
		return selected.FindLatestBackup(dbName)
	}

	// No location specified: auto-pick if unambiguous, otherwise ask - never guess.
	if len(candidates) == 1 {
		return candidates[0].FindLatestBackup(dbName)
	}

	names := make([]string, len(candidates))
	for i, loc := range candidates {
		names[i] = loc.Name
	}

	var chosen string
	prompt := &survey.Select{
		Message: fmt.Sprintf("Multiple locations found for '%s' - which one should be restored from?", dbName),
		Options: names,
	}
	if err := survey.AskOne(prompt, &chosen); err != nil {
		return "", err
	}

	for _, loc := range candidates {
		if loc.Name == chosen {
			return loc.FindLatestBackup(dbName)
		}
	}
	return "", fmt.Errorf("location '%s' not found", chosen)
}

func filterLocationsBySource(locations []*location.Config, sourceFilter string) []*location.Config {
	switch sourceFilter {
	case "", "all":
		return locations
	case "auto":
		var out []*location.Config
		for _, loc := range locations {
			if loc.Source == location.SourceSchedule {
				out = append(out, loc)
			}
		}
		return out
	case "manual":
		var out []*location.Config
		for _, loc := range locations {
			if loc.Source == location.SourceManual {
				out = append(out, loc)
			}
		}
		return out
	default:
		return locations
	}
}

func latestAcrossLocations(candidates []*location.Config, dbName string) (string, error) {
	var newestFile string
	var lastErr error
	for _, loc := range candidates {
		file, err := loc.FindLatestBackup(dbName)
		if err != nil {
			lastErr = err
			utils.Warning("Skipping location '%s': %v", loc.Name, err)
			continue
		}
		if newestFile == "" || file > newestFile {
			newestFile = file
		}
	}
	if newestFile == "" {
		if lastErr != nil {
			return "", fmt.Errorf("no restorable backups found across any location: %w", lastErr)
		}
		return "", fmt.Errorf("no restorable backups found across any location")
	}
	return newestFile, nil
}

func runVerifyBackup(cmd *cobra.Command, args []string) error {
	targetFilePath := restoreFile
	if len(args) > 0 {
		targetFilePath = args[0]
	}

	if targetFilePath == "" {
		return fmt.Errorf("backup file path is required (pass as positional argument or use --file flag)")
	}

	utils.Info("Inspecting backup file: %s...", targetFilePath)
	validation, err := restore.ValidateBackupFile(targetFilePath, expectedTypeFlag)
	if err != nil {
		return fmt.Errorf("backup validation error: %w", err)
	}

	fmt.Println()
	fmt.Println(restore.FormatValidationReport(validation))

	if !validation.IsValid {
		return fmt.Errorf("backup file verification failed")
	}

	utils.Success("Backup verification completed successfully!")
	return nil
}
