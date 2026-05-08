package internal

import (
	"github.com/spf13/cobra"
)

// rootCmd is the base command for the acrynite CLI.
var rootCmd = &cobra.Command{
	Use:   "acrynite",
	Short: "Acrynite CLI — manage your backup agents",
	Long: `Acrynite CLI lets you register, configure and monitor
backup agents connected to the Acrynite platform.`,
}

// Execute runs the root command. Call this from main.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(initCmd)
}
