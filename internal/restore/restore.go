package restore

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

// RestoreOptions holds configurations for a database restoration operation
type RestoreOptions struct {
	DatabaseName      string
	FilePath          string
	DryRun            bool
	CustomRestorePath string
}

// PerformRestore restores a SQL dump file into the specified database configuration
func PerformRestore(dbName, filePath string, options RestoreOptions) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	config, err := manager.Get(dbName)
	if err != nil {
		return fmt.Errorf("database '%s' not found: %w", dbName, err)
	}

	utils.Info("Starting restore pre-flight checks for '%s'...", dbName)

	// 1. Connection test
	utils.Info("Testing database connection to '%s'...", dbName)
	if err := manager.TestConnection(dbName); err != nil {
		return fmt.Errorf("connection test failed for target database: %w", err)
	}
	utils.Success("Connection to database '%s' established successfully", dbName)

	// 2. Validate backup file
	utils.Info("Validating backup file '%s'...", filePath)
	validation, err := ValidateBackupFile(filePath, config.Type)
	if err != nil {
		return fmt.Errorf("failed to validate backup file: %w", err)
	}

	if !validation.IsValid {
		return fmt.Errorf("backup file validation failed:\n%s", FormatValidationReport(validation))
	}

	utils.Success("Backup file is valid (%.2f MB, %d lines, detected: %s)", validation.SizeMB, validation.LineCount, validation.DetectedType)

	// 3. Resolve client restore binary (psql or mysql)
	clientBinary, err := resolveRestoreExecutable(config.Type, options.CustomRestorePath, config.DumpPath)
	if err != nil {
		return fmt.Errorf("failed to resolve restore tool: %w", err)
	}
	utils.Success("Resolved client restore binary: %s", clientBinary)

	// Dry run mode
	if options.DryRun {
		utils.Success("=== DRY RUN PASSED ===")
		utils.Info("Pre-flight checks succeeded. No database modifications were made.")
		utils.Info("Target Database: %s (%s at %s:%d)", config.Database, config.Type, config.Host, config.Port)
		utils.Info("Backup Source: %s (%.2f MB)", validation.FilePath, validation.SizeMB)
		utils.Info("Executable: %s", clientBinary)
		return nil
	}

	// 4. Perform actual restore
	startTime := time.Now()
	utils.Info("Executing database restoration...")

	var restoreErr error
	if config.Type == "mysql" {
		restoreErr = executeMySQLRestore(clientBinary, config, validation.FilePath)
	} else {
		restoreErr = executePostgresRestore(clientBinary, config, validation.FilePath)
	}

	if restoreErr != nil {
		return fmt.Errorf("restoration failed: %w", restoreErr)
	}

	duration := time.Since(startTime)
	utils.Success("Database restoration completed successfully in %s!", duration.Round(time.Millisecond))
	utils.Info("Restored '%s' into database '%s'", validation.FileName, config.Database)

	return nil
}

// executePostgresRestore runs psql to restore a PostgreSQL backup file
func executePostgresRestore(psqlPath string, config *database.DBConfig, sqlFilePath string) error {
	env := os.Environ()
	env = append(env, fmt.Sprintf("PGPASSWORD=%s", config.Password))

	args := []string{
		"-h", config.Host,
		"-p", fmt.Sprintf("%d", config.Port),
		"-U", config.User,
		"-d", config.Database,
		"-f", sqlFilePath,
	}

	cmd := exec.Command(psqlPath, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql execution failed: %w", err)
	}

	return nil
}

// executeMySQLRestore runs mysql client feeding the SQL dump via stdin
func executeMySQLRestore(mysqlPath string, config *database.DBConfig, sqlFilePath string) error {
	sqlFile, err := os.Open(sqlFilePath)
	if err != nil {
		return fmt.Errorf("failed to open backup file for restore: %w", err)
	}
	defer sqlFile.Close()

	args := []string{
		"-h", config.Host,
		"-P", fmt.Sprintf("%d", config.Port),
		"-u", config.User,
	}

	if config.Password != "" {
		args = append(args, fmt.Sprintf("-p%s", config.Password))
	}

	args = append(args, config.Database)

	cmd := exec.Command(mysqlPath, args...)
	cmd.Stdin = sqlFile
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysql execution failed: %w", err)
	}

	return nil
}

// resolveRestoreExecutable finds the psql or mysql binary across operating systems
func resolveRestoreExecutable(dbType string, customRestorePath string, dbConfigDumpPath string) (string, error) {
	commandName := "psql"
	if dbType == "mysql" {
		commandName = "mysql"
	}

	// 1. Explicit option custom path
	if strings.TrimSpace(customRestorePath) != "" {
		expanded := expandPathVars(customRestorePath)
		if info, err := os.Stat(expanded); err == nil && !info.IsDir() {
			return expanded, nil
		}
		return "", fmt.Errorf("custom restore path is invalid or not found: %s", customRestorePath)
	}

	// 2. Custom path from database config directory
	if strings.TrimSpace(dbConfigDumpPath) != "" {
		dir := filepath.Dir(dbConfigDumpPath)
		candidate := filepath.Join(dir, commandName)
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	// 3. Environment variable overrides
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
		}
	}

	// 4. System PATH lookup
	if commandPath, err := exec.LookPath(commandName); err == nil {
		return commandPath, nil
	}

	// 5. OS-specific standard installation locations
	var patterns []string
	if dbType == "mysql" {
		patterns = getMySQLClientSearchPatterns()
	} else {
		patterns = getPostgresClientSearchPatterns()
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
		sort.Slice(foundMatches, func(i, j int) bool {
			return foundMatches[i] > foundMatches[j]
		})
		return foundMatches[0], nil
	}

	return "", fmt.Errorf("%s client executable not found in PATH or standard installation locations. Add it to PATH or set %s_PATH", commandName, strings.ToUpper(commandName))
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

func getPostgresClientSearchPatterns() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			`%ProgramFiles%\PostgreSQL\*\bin\psql.exe`,
			`%ProgramFiles(x86)%\PostgreSQL\*\bin\psql.exe`,
			`%POSTGRESQL_HOME%\bin\psql.exe`,
			`C:\Program Files\PostgreSQL\*\bin\psql.exe`,
			`C:\Program Files (x86)\PostgreSQL\*\bin\psql.exe`,
			`C:\xampp\postgresql\bin\psql.exe`,
			`C:\laragon\bin\postgresql\*\bin\psql.exe`,
		}
	case "darwin":
		home := os.Getenv("HOME")
		return []string{
			"/opt/homebrew/bin/psql",
			"/opt/homebrew/opt/postgresql@*/bin/psql",
			"/opt/homebrew/opt/libpq/bin/psql",
			"/usr/local/bin/psql",
			"/usr/local/opt/postgresql@*/bin/psql",
			"/usr/local/opt/libpq/bin/psql",
			"/Applications/Postgres.app/Contents/Versions/*/bin/psql",
			"/Applications/MAMP/Library/bin/psql",
			home + "/Users/Shared/DBngin/postgresql/*/bin/psql",
		}
	default:
		return []string{
			"/usr/bin/psql",
			"/usr/local/bin/psql",
			"/usr/lib/postgresql/*/bin/psql",
			"/snap/bin/psql",
		}
	}
}

func getMySQLClientSearchPatterns() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			`C:\xampp\mysql\bin\mysql.exe`,
			`%ProgramFiles%\MySQL\MySQL Server *\bin\mysql.exe`,
			`%ProgramFiles(x86)%\MySQL\MySQL Server *\bin\mysql.exe`,
			`C:\Program Files\MySQL\MySQL Server *\bin\mysql.exe`,
			`C:\Program Files (x86)\MySQL\MySQL Server *\bin\mysql.exe`,
			`C:\laragon\bin\mysql\*\bin\mysql.exe`,
			`C:\wamp64\bin\mysql\mysql*\bin\mysql.exe`,
		}
	case "darwin":
		home := os.Getenv("HOME")
		return []string{
			"/opt/homebrew/bin/mysql",
			"/opt/homebrew/opt/mysql*/bin/mysql",
			"/opt/homebrew/opt/mariadb*/bin/mysql",
			"/usr/local/bin/mysql",
			"/usr/local/opt/mysql*/bin/mysql",
			"/usr/local/opt/mariadb*/bin/mysql",
			"/Applications/MAMP/Library/bin/mysql",
			home + "/Users/Shared/DBngin/mysql/*/bin/mysql",
		}
	default:
		return []string{
			"/usr/bin/mysql",
			"/usr/local/bin/mysql",
			"/usr/lib/mysql/bin/mysql",
			"/snap/bin/mysql",
		}
	}
}
