package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// rootCmd represents the base command when called without any subcommands
	rootCmd = &cobra.Command{
		Use:   "acrynite-cli",
		Short: "Acrynite CLI - A powerful command-line tool",
		Long: `Acrynite CLI is a command-line interface tool that provides
various utilities and commands for your workflow.

Use this tool to streamline your development process and automate
common tasks with ease.`,
		Run: func(cmd *cobra.Command, args []string) {
			// Default behavior when no subcommand is specified
			fmt.Println("Welcome to Acrynite CLI!")
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
	rootCmd.AddCommand(dbCmd)
}




