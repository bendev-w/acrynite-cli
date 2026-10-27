package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// rootCmd represents the base command when called without any subcommands
	rootCmd = &cobra.Command{
		Use:   "acrynite",
		Short: "PostgreSQL backup and recovery CLI",
		Long:  "Acrynite registers PostgreSQL clusters and S3 destinations, then manages backups and recovery metadata.",
		// Runtime failures are not usage mistakes; Execute prints the error once.
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			// Default behavior when no subcommand is specified
			fmt.Println("Acrynite PostgreSQL backup and recovery CLI")
			fmt.Println("Use --help to see available commands.")
		},
	}

	// Global flags
	verbose bool
	config  string
)

// init initializes the root command and sets up global flags
func init() {
	// Register global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVar(&config, "config", "", "config file (default is $HOME/.acrynite-cli.yaml)")

	// Register subcommands
	registerCommands()
}

// registerCommands adds all subcommands to the root command
func registerCommands() {
	// Register version command
	rootCmd.AddCommand(versionCmd)

	// add command to register db
	rootCmd.AddCommand(databaseCmd)
	rootCmd.AddCommand(storageCmd)

	// add command for backups
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(scheduleCmd)
	rootCmd.AddCommand(walCmd)
	rootCmd.AddCommand(recoveryPointsCmd)
	rootCmd.AddCommand(restoreCmd)
}
