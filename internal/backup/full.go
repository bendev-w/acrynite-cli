package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
	pgDumpPath, err := resolveDumpExecutable("postgres", config.DumpPath)
	if err != nil {
		return err
	}

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
	cmd := exec.Command(pgDumpPath, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute backup
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w", err)
	}

	return nil
}

// PerformMySQLDump performs a MySQL dump using mysqldump command
func PerformMySQLDump(config *database.DBConfig, outputFile string) error {
	mysqlDumpPath, err := resolveDumpExecutable("mysql", config.DumpPath)
	if err != nil {
		return err
	}

	// Build mysqldump command
	args := []string{
		"-h", config.Host,
		"-P", fmt.Sprintf("%d", config.Port),
		"-u", config.User,
	}

	// Only add password flag if password is not empty.
	// If omitted, mysqldump connects without password (common local dev setup).
	if config.Password != "" {
		args = append(args, fmt.Sprintf("-p%s", config.Password))
	}

	args = append(args, config.Database, "--result-file="+outputFile)

	// Create command
	cmd := exec.Command(mysqlDumpPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute backup
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysqldump failed: %w", err)
	}

	return nil
}

// resolveDumpExecutable resolves the binary path for pg_dump or mysqldump across Linux, macOS, and Windows.
func resolveDumpExecutable(dbType string, customDumpPath string) (string, error) {
	commandName := "pg_dump"
	if dbType == "mysql" {
		commandName = "mysqldump"
	}
	return resolveExecutable(commandName, dbType, customDumpPath)
}

// resolveExecutable resolves a PostgreSQL or MySQL client binary by name. A custom path
// may point at the binary itself or, for sibling tools like pg_basebackup, at a sibling
// binary in the same directory.
func resolveExecutable(commandName, dbType, customDumpPath string) (string, error) {
	// 1. Custom explicit dump path from database configuration
	if strings.TrimSpace(customDumpPath) != "" {
		expanded := expandPathVars(customDumpPath)
		if info, err := os.Stat(expanded); err == nil && !info.IsDir() {
			if baseNameWithoutExe(expanded) == commandName {
				return expanded, nil
			}
			sibling := filepath.Join(filepath.Dir(expanded), commandName+filepath.Ext(expanded))
			if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
				return sibling, nil
			}
			if commandName == "pg_dump" || commandName == "mysqldump" {
				return expanded, nil
			}
			return "", fmt.Errorf("%s not found next to configured dump path %s", commandName, customDumpPath)
		}
		return "", fmt.Errorf("configured custom dump path for %s is invalid or not found: %s", commandName, customDumpPath)
	}

	// 2. Environment variable overrides
	envVars := []string{
		fmt.Sprintf("ACRYNITE_%s_PATH", strings.ToUpper(commandName)),
		fmt.Sprintf("%s_PATH", strings.ToUpper(commandName)),
	}
	for _, envVar := range envVars {
		if envVal := strings.TrimSpace(os.Getenv(envVar)); envVal != "" {
			expanded := expandPathVars(envVal)
			if info, err := os.Stat(expanded); err == nil && !info.IsDir() {
				return expanded, nil
			}
			return "", fmt.Errorf("environment variable %s is set but points to invalid file: %s", envVar, envVal)
		}
	}

	// 3. System PATH lookup
	if commandPath, err := exec.LookPath(commandName); err == nil {
		return commandPath, nil
	}

	// 4. OS-specific standard installation locations & dynamic version globs
	var patterns []string
	if dbType == "mysql" {
		patterns = getMySQLSearchPatterns()
	} else {
		for _, pattern := range getPostgresSearchPatterns() {
			patterns = append(patterns, strings.ReplaceAll(pattern, "pg_dump", commandName))
		}
	}

	var foundMatches []string
	for _, pattern := range patterns {
		expanded := expandPathVars(pattern)
		matches, err := filepath.Glob(expanded)
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && !info.IsDir() {
				foundMatches = append(foundMatches, match)
			}
		}
	}

	if len(foundMatches) > 0 {
		// Sort descending so highest version is preferred (e.g., PostgreSQL 16 > 15)
		sort.Slice(foundMatches, func(i, j int) bool {
			return foundMatches[i] > foundMatches[j]
		})
		return foundMatches[0], nil
	}

	envVarName := fmt.Sprintf("%s_PATH", strings.ToUpper(commandName))
	return "", fmt.Errorf("%s executable not found in system PATH or standard %s installation locations. Please add it to your system PATH or set the %s environment variable", commandName, strings.ToUpper(runtime.GOOS), envVarName)
}

func baseNameWithoutExe(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func expandPathVars(path string) string {
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "SystemDrive", "LOCALAPPDATA", "APPDATA", "MYSQL_HOME", "POSTGRESQL_HOME", "PGDATA"} {
			val := os.Getenv(env)
			if val != "" {
				path = strings.ReplaceAll(path, "%"+env+"%", val)
				path = strings.ReplaceAll(path, "%"+strings.ToLower(env)+"%", val)
			}
		}
	}
	return os.ExpandEnv(path)
}

func getPostgresSearchPatterns() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			`%ProgramFiles%\PostgreSQL\*\bin\pg_dump.exe`,
			`%ProgramFiles(x86)%\PostgreSQL\*\bin\pg_dump.exe`,
			`%POSTGRESQL_HOME%\bin\pg_dump.exe`,
			`%PGDATA%\..\bin\pg_dump.exe`,
			`C:\Program Files\PostgreSQL\*\bin\pg_dump.exe`,
			`C:\Program Files (x86)\PostgreSQL\*\bin\pg_dump.exe`,
			`C:\xampp\postgresql\bin\pg_dump.exe`,
			`C:\laragon\bin\postgresql\*\bin\pg_dump.exe`,
		}
	case "darwin": // macOS
		home := os.Getenv("HOME")
		return []string{
			"/opt/homebrew/bin/pg_dump",
			"/opt/homebrew/opt/postgresql@*/bin/pg_dump",
			"/opt/homebrew/opt/libpq/bin/pg_dump",
			"/usr/local/bin/pg_dump",
			"/usr/local/opt/postgresql@*/bin/pg_dump",
			"/usr/local/opt/libpq/bin/pg_dump",
			"/opt/local/bin/pg_dump",
			"/opt/local/lib/postgresql*/bin/pg_dump",
			"/Applications/Postgres.app/Contents/Versions/*/bin/pg_dump",
			"/Applications/MAMP/Library/bin/pg_dump",
			home + "/Users/Shared/DBngin/postgresql/*/bin/pg_dump",
			"/Users/Shared/DBngin/postgresql/*/bin/pg_dump",
			home + "/Library/Application Support/DBngin/postgresql/*/bin/pg_dump",
		}
	default: // Linux and others
		return []string{
			"/usr/bin/pg_dump",
			"/usr/local/bin/pg_dump",
			"/usr/lib/postgresql/*/bin/pg_dump",
			"/snap/bin/pg_dump",
			"/opt/lampp/bin/pg_dump",
		}
	}
}

func getMySQLSearchPatterns() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			`C:\xampp\mysql\bin\mysqldump.exe`,
			`%ProgramFiles%\MySQL\MySQL Server *\bin\mysqldump.exe`,
			`%ProgramFiles(x86)%\MySQL\MySQL Server *\bin\mysqldump.exe`,
			`C:\Program Files\MySQL\MySQL Server *\bin\mysqldump.exe`,
			`C:\Program Files (x86)\MySQL\MySQL Server *\bin\mysqldump.exe`,
			`C:\laragon\bin\mysql\*\bin\mysqldump.exe`,
			`C:\wamp64\bin\mysql\mysql*\bin\mysqldump.exe`,
			`C:\wamp\bin\mysql\mysql*\bin\mysqldump.exe`,
			`%MYSQL_HOME%\bin\mysqldump.exe`,
		}
	case "darwin": // macOS
		home := os.Getenv("HOME")
		return []string{
			"/opt/homebrew/bin/mysqldump",
			"/opt/homebrew/opt/mysql*/bin/mysqldump",
			"/opt/homebrew/opt/mariadb*/bin/mysqldump",
			"/usr/local/bin/mysqldump",
			"/usr/local/opt/mysql*/bin/mysqldump",
			"/usr/local/opt/mariadb*/bin/mysqldump",
			"/opt/local/bin/mysqldump",
			"/opt/local/lib/mysql*/bin/mysqldump",
			"/Applications/MAMP/Library/bin/mysqldump",
			home + "/Users/Shared/DBngin/mysql/*/bin/mysqldump",
			"/Users/Shared/DBngin/mysql/*/bin/mysqldump",
			home + "/Library/Application Support/DBngin/mysql/*/bin/mysqldump",
		}
	default: // Linux and others
		return []string{
			"/usr/bin/mysqldump",
			"/usr/local/bin/mysqldump",
			"/usr/lib/mysql/bin/mysqldump",
			"/snap/bin/mysqldump",
			"/opt/lampp/bin/mysqldump",
		}
	}
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
