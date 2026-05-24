package cmd

import (
	"fmt"
	"os"
)

// Execute is the main entry point for the CLI application
// It initializes and executes the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
