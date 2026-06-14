package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"acrynite-cli/internal/database"
	"acrynite-cli/utils"
)

// PerformBackup executes a full backup for the specified database
func PerformBackup(dbName, backupPath string) error {
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
	var backupErr error
	if config.Type == "mysql" {
		backupErr = PerformMySQLDump(config, backupFile)
	} else {
		backupErr = PerformPostgresDump(config, backupFile)
	}

	if backupErr != nil {
		return fmt.Errorf("backup failed: %w", backupErr)
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

// PerformPostgresDump performs a PostgreSQL dump using pg_dump command
func PerformPostgresDump(config *database.DBConfig, outputFile string) error {
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

// PerformMySQLDump performs a MySQL dump using mysqldump command
func PerformMySQLDump(config *database.DBConfig, outputFile string) error {
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

// ParseInterval converts interval string to time.Duration
func ParseInterval(interval string) (time.Duration, error) {
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
