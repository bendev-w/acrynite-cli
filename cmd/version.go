package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	// Version information - these can be set at build time using ldflags
	// Example: go build -ldflags "-X acrynite-cli/cmd.Version=1.0.0"
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long: `Display the current version of Acrynite CLI along with
build information including Git commit hash and build date.`,
	Run: func(cmd *cobra.Command, args []string) {
		printVersion()
	},
}

// printVersion displays formatted version information
func printVersion() {
	fmt.Printf("Acrynite CLI\n")
	fmt.Printf("  Version:    %s\n", Version)
	fmt.Printf("  Git Commit: %s\n", GitCommit)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	fmt.Printf("  Go Version: %s\n", runtime.Version())
	fmt.Printf("  OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
