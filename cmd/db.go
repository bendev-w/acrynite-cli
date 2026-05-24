package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"acrynite-cli/internal/database"
	"acrynite-cli/utils"
	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
)

var (
	dbName     string
	dbHost     string
	dbPort     int
	dbUser     string
	dbPassword string
	dbDatabase string
	dbSSLMode  string
	forceFlag  bool
	exportFile string
	importFile string
)

// dbCmd represents the database command
var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Manage database connections",
	Long:  `Manage PostgreSQL database connections. Register, list, and test database configurations.`,
}

// dbRegisterCmd handles database registration
var dbRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new database",
	Long: `Register a new PostgreSQL database configuration with the CLI.
The configuration will be stored locally and can be used for backups and operations.`,
	Example: `  acrynite db register \
    --name production-db \
    --host localhost \
    --port 5432 \
    --user postgres \
    --password secret \
    --database app_db`,
	RunE: runDBRegister,
}

// dbListCmd lists all registered databases
var dbListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all registered databases",
	Aliases: []string{"ls"},
	RunE:    runDBList,
}

// dbConnectCmd tests a database connection
var dbConnectCmd = &cobra.Command{
	Use:   "connect [name]",
	Short: "Test database connection",
	Args:  cobra.ExactArgs(1),
	RunE:  runDBConnect,
}

// dbShowCmd shows database details
var dbShowCmd = &cobra.Command{
	Use:     "show [name]",
	Short:   "Show database configuration details",
	Aliases: []string{"get", "info"},
	Args:    cobra.ExactArgs(1),
	RunE:    runDBShow,
}

// dbUpdateCmd updates a database configuration
var dbUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Update database configuration",
	Long: `Update an existing database configuration. You can update individual fields
by providing the corresponding flags. Fields not specified will remain unchanged.`,
	Example: `  acrynite db update production-db --host new-host.com --port 5433
  acrynite db update dev-db --password newpassword`,
	Args: cobra.ExactArgs(1),
	RunE: runDBUpdate,
}

// dbRemoveCmd removes a database configuration
var dbRemoveCmd = &cobra.Command{
	Use:     "remove [name]",
	Short:   "Remove database configuration",
	Aliases: []string{"delete", "rm"},
	Args:    cobra.ExactArgs(1),
	RunE:    runDBRemove,
}

// dbExportCmd exports database configurations
var dbExportCmd = &cobra.Command{
	Use:   "export [file]",
	Short: "Export database configurations to a file",
	Long:  `Export all database configurations to a JSON file for backup or sharing.`,
	Example: `  acrynite db export backup.json
  acrynite db export /path/to/db-configs.json`,
	Args: cobra.ExactArgs(1),
	RunE: runDBExport,
}

// dbImportCmd imports database configurations
var dbImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import database configurations from a file",
	Long: `Import database configurations from a JSON file.
Existing databases with the same name will be skipped by default.`,
	Example: `  acrynite db import backup.json
  acrynite db import /path/to/db-configs.json`,
	Args: cobra.ExactArgs(1),
	RunE: runDBImport,
}

func init() {
	// Add subcommands
	dbCmd.AddCommand(dbRegisterCmd)
	dbCmd.AddCommand(dbListCmd)
	dbCmd.AddCommand(dbConnectCmd)
	dbCmd.AddCommand(dbShowCmd)
	dbCmd.AddCommand(dbUpdateCmd)
	dbCmd.AddCommand(dbRemoveCmd)
	dbCmd.AddCommand(dbExportCmd)
	dbCmd.AddCommand(dbImportCmd)

	// Register command flags
	dbRegisterCmd.Flags().StringVar(&dbName, "name", "", "Unique database identifier (required)")
	dbRegisterCmd.Flags().StringVar(&dbHost, "host", "", "Database host (required)")
	dbRegisterCmd.Flags().IntVar(&dbPort, "port", 5432, "Database port")
	dbRegisterCmd.Flags().StringVar(&dbUser, "user", "", "Database user (required)")
	dbRegisterCmd.Flags().StringVar(&dbPassword, "password", "", "Database password (required)")
	dbRegisterCmd.Flags().StringVar(&dbDatabase, "database", "", "Database name (required)")
	dbRegisterCmd.Flags().StringVar(&dbSSLMode, "ssl-mode", "disable", "SSL mode (disable, require, verify-ca, verify-full)")

	// Update command flags
	dbUpdateCmd.Flags().StringVar(&dbName, "name", "", "New database identifier")
	dbUpdateCmd.Flags().StringVar(&dbHost, "host", "", "New database host")
	dbUpdateCmd.Flags().IntVar(&dbPort, "port", 0, "New database port")
	dbUpdateCmd.Flags().StringVar(&dbUser, "user", "", "New database user")
	dbUpdateCmd.Flags().StringVar(&dbPassword, "password", "", "New database password")
	dbUpdateCmd.Flags().StringVar(&dbDatabase, "database", "", "New database name")
	dbUpdateCmd.Flags().StringVar(&dbSSLMode, "ssl-mode", "", "New SSL mode (disable, require, verify-ca, verify-full)")

	// Remove command flags
	dbRemoveCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip confirmation prompt")
}

func runDBRegister(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	// If flags are not provided, prompt interactively
	if dbName == "" || dbHost == "" || dbUser == "" || dbPassword == "" || dbDatabase == "" {
		utils.Info("Interactive database registration")
		fmt.Println()

		if err := promptDatabaseDetails(); err != nil {
			return err
		}
	}

	// Validate required fields
	if dbName == "" {
		return fmt.Errorf("database name is required")
	}
	if dbHost == "" {
		return fmt.Errorf("host is required")
	}
	if dbUser == "" {
		return fmt.Errorf("user is required")
	}
	if dbPassword == "" {
		return fmt.Errorf("password is required")
	}
	if dbDatabase == "" {
		return fmt.Errorf("database name is required")
	}

	// Create configuration
	config := database.NewDBConfig(dbName, dbHost, dbPort, dbUser, dbPassword, dbDatabase, dbSSLMode)

	// Register the database
	utils.Info("Registering database: %s", dbName)
	if err := manager.Register(config); err != nil {
		utils.Error("Failed to register database: %v", err)
		return err
	}

	// Display success
	utils.Success("Database '%s' registered successfully!", dbName)
	fmt.Println()

	// Automatically test connection
	utils.Info("Testing database connection...")
	if err := manager.TestConnection(dbName); err != nil {
		utils.Warning("Connection test failed: %v", err)
		utils.Info("Database registered but marked as inactive. Fix the connection details and run: acrynite db test %s", dbName)
	} else {
		utils.Success("Connection successful! Database is active.")
	}

	fmt.Println()
	displayDatabaseInfo(config)

	return nil
}

func runDBList(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	configs := manager.List()

	if len(configs) == 0 {
		utils.Info("No databases registered")
		utils.Info("Register a database with: acrynite db register")
		return nil
	}

	utils.Info("Registered databases: %d", len(configs))
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tPORT\tDATABASE\tUSER\tSTATUS")
	fmt.Fprintln(w, "----\t----\t----\t--------\t----\t------")

	for _, config := range configs {
		status := "✓ Active"
		if !config.IsActive {
			status = "○ Inactive"
		}

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
			config.Name,
			config.Host,
			config.Port,
			config.Database,
			config.User,
			status,
		)
	}

	w.Flush()
	return nil
}

func runDBConnect(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	name := args[0]
	if err := manager.TestConnection(name); err != nil {
		utils.Error("Connection test failed: %v", err)
		return err
	}

	utils.Success("Connection to '%s' successful!", name)
	return nil
}

func promptDatabaseDetails() error {
	questions := []*survey.Question{
		{
			Name: "name",
			Prompt: &survey.Input{
				Message: "Database identifier (unique name):",
				Help:    "A unique name to identify this database (e.g., production-db)",
			},
			Validate: survey.Required,
		},
		{
			Name: "host",
			Prompt: &survey.Input{
				Message: "Database host:",
				Default: "localhost",
			},
			Validate: survey.Required,
		},
		{
			Name: "port",
			Prompt: &survey.Input{
				Message: "Database port:",
				Default: "5432",
			},
			Validate: survey.Required,
		},
		{
			Name: "user",
			Prompt: &survey.Input{
				Message: "Database user:",
				Default: "postgres",
			},
			Validate: survey.Required,
		},
		{
			Name: "password",
			Prompt: &survey.Password{
				Message: "Database password:",
			},
			Validate: survey.Required,
		},
		{
			Name: "database",
			Prompt: &survey.Input{
				Message: "Database name:",
			},
			Validate: survey.Required,
		},
		{
			Name: "sslmode",
			Prompt: &survey.Select{
				Message: "SSL mode:",
				Options: []string{"disable", "require", "verify-ca", "verify-full"},
				Default: "disable",
			},
		},
	}

	answers := struct {
		Name     string
		Host     string
		Port     string
		User     string
		Password string
		Database string
		SSLMode  string `survey:"sslmode"`
	}{}

	if err := survey.Ask(questions, &answers); err != nil {
		return err
	}

	dbName = answers.Name
	dbHost = answers.Host
	dbPort = 5432
	fmt.Sscanf(answers.Port, "%d", &dbPort)
	dbUser = answers.User
	dbPassword = answers.Password
	dbDatabase = answers.Database
	dbSSLMode = answers.SSLMode

	return nil
}

func displayDatabaseInfo(config *database.DBConfig) {
	fmt.Println("Database Configuration:")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("  Name:           %s\n", config.Name)
	fmt.Printf("  Host:           %s\n", config.Host)
	fmt.Printf("  Port:           %d\n", config.Port)
	fmt.Printf("  Database:       %s\n", config.Database)
	fmt.Printf("  User:           %s\n", config.User)
	fmt.Printf("  SSL Mode:       %s\n", config.SSLMode)
	fmt.Printf("  Status:         %s\n", func() string {
		if config.IsActive {
			return "Active"
		}
		return "Inactive"
	}())
	fmt.Printf("  Created:        %s (%s)\n",
		utils.FormatTimestamp(config.CreatedAt, utils.TimestampLog),
		utils.TimeAgo(config.CreatedAt))
	fmt.Println(strings.Repeat("=", 50))
}

func runDBShow(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	name := args[0]
	if name == "" {
		return fmt.Errorf("database name is required")
	}

	config, err := manager.Get(name)
	if err != nil {
		utils.Error("Database not found: %v", err)
		return err
	}

	fmt.Println()
	displayDatabaseInfo(config)
	fmt.Println()

	// Show connection string (masked)
	utils.Info("Connection String:")
	fmt.Printf("  %s\n", config.SafeConnectionString())

	// Show last tested time if available
	if !config.LastTested.IsZero() {
		fmt.Println()
		utils.Info("Last Tested:")
		fmt.Printf("  %s (%s)\n",
			utils.FormatTimestamp(config.LastTested, utils.TimestampLog),
			utils.TimeAgo(config.LastTested))
	}

	return nil
}

func runDBUpdate(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	oldName := args[0]
	if oldName == "" {
		return fmt.Errorf("database name is required")
	}

	// Check if database exists
	if !manager.Exists(oldName) {
		return fmt.Errorf("database '%s' not found", oldName)
	}

	// Get existing configuration
	existingConfig, err := manager.Get(oldName)
	if err != nil {
		return err
	}

	// Check if any flags were provided
	flags := cmd.Flags()
	hasChanges := false

	// Create a new config based on existing
	newName := existingConfig.Name
	newHost := existingConfig.Host
	newPort := existingConfig.Port
	newUser := existingConfig.User
	newPassword := existingConfig.Password
	newDatabase := existingConfig.Database
	newSSLMode := existingConfig.SSLMode

	// Update only provided flags
	if flags.Changed("name") {
		newName = dbName
		hasChanges = true
		if newName == "" {
			return fmt.Errorf("database name cannot be empty")
		}
	}
	if flags.Changed("host") {
		newHost = dbHost
		hasChanges = true
		if newHost == "" {
			return fmt.Errorf("host cannot be empty")
		}
	}
	if flags.Changed("port") {
		newPort = dbPort
		hasChanges = true
		if newPort <= 0 || newPort > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
		}
	}
	if flags.Changed("user") {
		newUser = dbUser
		hasChanges = true
		if newUser == "" {
			return fmt.Errorf("user cannot be empty")
		}
	}
	if flags.Changed("password") {
		newPassword = dbPassword
		hasChanges = true
		if newPassword == "" {
			return fmt.Errorf("password cannot be empty")
		}
	}
	if flags.Changed("database") {
		newDatabase = dbDatabase
		hasChanges = true
		if newDatabase == "" {
			return fmt.Errorf("database name cannot be empty")
		}
	}
	if flags.Changed("ssl-mode") {
		newSSLMode = dbSSLMode
		hasChanges = true
	}

	if !hasChanges {
		utils.Warning("No changes specified. Use flags to update database configuration.")
		utils.Info("Example: acrynite db update %s --host new-host.com --port 5433", oldName)
		return nil
	}

	// Create updated configuration
	updatedConfig := database.NewDBConfig(newName, newHost, newPort, newUser, newPassword, newDatabase, newSSLMode)
	updatedConfig.CreatedAt = existingConfig.CreatedAt
	
	if flags.Changed("host") || flags.Changed("port") || flags.Changed("user") || 
	   flags.Changed("password") || flags.Changed("database") || flags.Changed("ssl-mode") {
		updatedConfig.IsActive = false
	} else {
		// Preserve active status if only name changed
		updatedConfig.IsActive = existingConfig.IsActive
	}

	// Update the database
	utils.Info("Updating database: %s", oldName)
	if err := manager.Update(oldName, updatedConfig); err != nil {
		utils.Error("Failed to update database: %v", err)
		return err
	}

	utils.Success("Database '%s' updated successfully!", newName)
	fmt.Println()

	// Automatically test connection if connection details changed
	if flags.Changed("host") || flags.Changed("port") || flags.Changed("user") ||
		flags.Changed("password") || flags.Changed("database") || flags.Changed("ssl-mode") {
		utils.Info("Testing database connection...")
		if err := manager.TestConnection(newName); err != nil {
			utils.Warning("Connection test failed: %v", err)
			utils.Info("Database updated but marked as inactive. Fix the connection details and run: acrynite db test %s", newName)
		} else {
			utils.Success("Connection successful! Database is active.")
		}
		fmt.Println()
	}

	displayDatabaseInfo(updatedConfig)

	return nil
}

func runDBRemove(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	name := args[0]
	if name == "" {
		return fmt.Errorf("database name is required")
	}

	// Check if database exists
	if !manager.Exists(name) {
		return fmt.Errorf("database '%s' not found", name)
	}

	// Confirm deletion unless --force flag is used
	if !forceFlag {
		var confirm bool
		prompt := &survey.Confirm{
			Message: fmt.Sprintf("Are you sure you want to remove database '%s'?", name),
			Default: false,
		}
		if err := survey.AskOne(prompt, &confirm); err != nil {
			return err
		}

		if !confirm {
			utils.Info("Operation cancelled")
			return nil
		}
	}

	// Remove the database
	utils.Info("Removing database: %s", name)
	if err := manager.Remove(name); err != nil {
		utils.Error("Failed to remove database: %v", err)
		return err
	}

	utils.Success("Database '%s' removed successfully!", name)
	return nil
}

func runDBExport(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	filePath := args[0]
	if filePath == "" {
		return fmt.Errorf("export file path is required")
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(filePath), ".json") {
		utils.Warning("File does not have .json extension, adding it")
		filePath = filePath + ".json"
	}

	// Check if file exists
	if utils.Exists(filePath) {
		var overwrite bool
		prompt := &survey.Confirm{
			Message: fmt.Sprintf("File '%s' already exists. Overwrite?", filePath),
			Default: false,
		}
		if err := survey.AskOne(prompt, &overwrite); err != nil {
			return err
		}

		if !overwrite {
			utils.Info("Export cancelled")
			return nil
		}
	}

	// Export configurations
	utils.Info("Exporting database configurations to: %s", filePath)
	if err := manager.Export(filePath); err != nil {
		utils.Error("Failed to export configurations: %v", err)
		return err
	}

	configs := manager.List()
	utils.Success("Exported %d database configuration(s) to %s", len(configs), filePath)
	return nil
}

func runDBImport(cmd *cobra.Command, args []string) error {
	manager, err := database.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get config manager: %w", err)
	}

	filePath := args[0]
	if filePath == "" {
		return fmt.Errorf("import file path is required")
	}

	// Validate file exists
	if !utils.Exists(filePath) {
		return fmt.Errorf("file '%s' does not exist", filePath)
	}

	// Validate file is readable
	if !utils.IsFile(filePath) {
		return fmt.Errorf("'%s' is not a file", filePath)
	}

	// Import configurations
	utils.Info("Importing database configurations from: %s", filePath)
	if err := manager.Import(filePath); err != nil {
		utils.Error("Failed to import configurations: %v", err)
		return err
	}

	return nil
}
