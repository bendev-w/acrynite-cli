package cmd

import (
	"fmt"

	"acrynite-cli/internal/location"
	"acrynite-cli/internal/provider"
	"acrynite-cli/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
)

var (
	providerName      string
	providerAccessKey string
	providerSecretKey string
	providerRegion    string
	providerBucket    string
	providerEndpoint  string
	providerForceFlag bool
)

// providerCmd manages authenticated cloud storage accounts used by backup locations.
var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage cloud storage provider credentials",
	Long: `Register and manage authenticated cloud accounts (Google Drive, S3, etc.)
that backup locations can use as a destination.`,
}

var providerAddCmd = &cobra.Command{
	Use:   "add [gdrive|s3]",
	Short: "Register a new cloud storage provider",
	Args:  cobra.ExactArgs(1),
	Example: `  acrynite provider add gdrive --name=personal-drive
  acrynite provider add s3 --name=work-bucket`,
	RunE: runProviderAdd,
}

var providerListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List registered providers",
	RunE:    runProviderList,
}

var providerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check that provider credentials still work",
	RunE:  runProviderStatus,
}

var providerReauthCmd = &cobra.Command{
	Use:   "reauth <name>",
	Short: "Re-run the auth flow for a provider whose token expired",
	Args:  cobra.ExactArgs(1),
	RunE:  runProviderReauth,
}

var providerRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a provider's stored credentials and metadata",
	Args:  cobra.ExactArgs(1),
	RunE:  runProviderRemove,
}

func init() {
	rootCmd.AddCommand(providerCmd)

	providerCmd.AddCommand(providerAddCmd)
	providerCmd.AddCommand(providerListCmd)
	providerCmd.AddCommand(providerStatusCmd)
	providerCmd.AddCommand(providerReauthCmd)
	providerCmd.AddCommand(providerRemoveCmd)

	providerAddCmd.Flags().StringVar(&providerName, "name", "", "Name to register this provider under (required)")
	providerAddCmd.Flags().StringVar(&providerAccessKey, "access-key", "", "S3 access key ID (prompted interactively if omitted)")
	providerAddCmd.Flags().StringVar(&providerSecretKey, "secret-key", "", "S3 secret access key (prompted interactively if omitted)")
	providerAddCmd.Flags().StringVar(&providerRegion, "region", "", "S3 region")
	providerAddCmd.Flags().StringVar(&providerBucket, "bucket", "", "S3 bucket name")
	providerAddCmd.Flags().StringVar(&providerEndpoint, "endpoint", "", "S3-compatible endpoint (optional, non-AWS services)")
	_ = providerAddCmd.MarkFlagRequired("name")

	providerRemoveCmd.Flags().BoolVar(&providerForceFlag, "force", false, "Remove even if locations still reference this provider")
}

func runProviderAdd(cmd *cobra.Command, args []string) error {
	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get provider manager: %w", err)
	}

	if providerMgr.Exists(providerName) {
		return fmt.Errorf("provider '%s' already exists", providerName)
	}

	switch provider.Type(args[0]) {
	case provider.TypeGDrive:
		utils.Warning("Google OAuth is not implemented in this build yet.")
		utils.Warning("Registering '%s' as a placeholder - it cannot be used by schedules until OAuth support lands.", providerName)

		cfg := &provider.Config{
			Name:   providerName,
			Type:   provider.TypeGDrive,
			Status: provider.StatusNotConfigured,
			Note:   "OAuth flow not implemented yet; run 'acrynite provider reauth' once available",
		}
		if err := providerMgr.Register(cfg); err != nil {
			return err
		}
		utils.Success("Provider '%s' registered (not_configured)", providerName)
		return nil

	case provider.TypeS3:
		if providerAccessKey == "" {
			prompt := &survey.Input{Message: "AWS access key ID:"}
			if err := survey.AskOne(prompt, &providerAccessKey, survey.WithValidator(survey.Required)); err != nil {
				return err
			}
		} else {
			utils.Warning("--access-key was passed on the command line; it may be visible in shell history")
		}

		if providerSecretKey == "" {
			prompt := &survey.Password{Message: "AWS secret access key:"}
			if err := survey.AskOne(prompt, &providerSecretKey, survey.WithValidator(survey.Required)); err != nil {
				return err
			}
		} else {
			utils.Warning("--secret-key was passed on the command line; it may be visible in shell history")
		}

		if providerRegion == "" {
			prompt := &survey.Input{Message: "Region:", Default: "us-east-1"}
			if err := survey.AskOne(prompt, &providerRegion); err != nil {
				return err
			}
		}

		if providerBucket == "" {
			prompt := &survey.Input{Message: "Bucket name:"}
			if err := survey.AskOne(prompt, &providerBucket, survey.WithValidator(survey.Required)); err != nil {
				return err
			}
		}

		// NOTE: a lightweight connectivity check (e.g. HeadBucket) should run here
		// before saving anything, once the AWS SDK is added as a dependency.
		utils.Warning("Skipping live bucket connectivity check - AWS SDK integration not added yet")

		secretStore, err := provider.NewSecretStore()
		if err != nil {
			return fmt.Errorf("failed to initialize secret store: %w", err)
		}
		if err := secretStore.Set(providerName+":access_key", providerAccessKey); err != nil {
			return fmt.Errorf("failed to store access key: %w", err)
		}
		if err := secretStore.Set(providerName+":secret_key", providerSecretKey); err != nil {
			return fmt.Errorf("failed to store secret key: %w", err)
		}

		cfg := &provider.Config{
			Name:     providerName,
			Type:     provider.TypeS3,
			Region:   providerRegion,
			Bucket:   providerBucket,
			Endpoint: providerEndpoint,
			Status:   provider.StatusUnverified,
			Note:     "Connectivity not verified; AWS SDK integration not added yet",
		}
		if err := providerMgr.Register(cfg); err != nil {
			return err
		}
		utils.Success("Provider '%s' registered (unverified)", providerName)
		return nil

	default:
		return fmt.Errorf("unsupported provider type '%s' (expected 'gdrive' or 's3')", args[0])
	}
}

func runProviderList(cmd *cobra.Command, args []string) error {
	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get provider manager: %w", err)
	}

	providers := providerMgr.List()
	if len(providers) == 0 {
		utils.Info("No providers registered")
		utils.Info("Register one with: acrynite provider add gdrive|s3 --name=<name>")
		return nil
	}

	utils.Info("Registered Providers (%d):", len(providers))
	fmt.Println()
	for _, p := range providers {
		fmt.Printf("  Name:     %s\n", p.Name)
		fmt.Printf("  Type:     %s\n", p.Type)
		fmt.Printf("  Status:   %s\n", p.Status)
		if p.Account != "" {
			fmt.Printf("  Account:  %s\n", p.Account)
		}
		if p.Bucket != "" {
			fmt.Printf("  Bucket:   %s\n", p.Bucket)
		}
		if p.Note != "" {
			fmt.Printf("  Note:     %s\n", p.Note)
		}
		fmt.Println()
	}
	return nil
}

func runProviderStatus(cmd *cobra.Command, args []string) error {
	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get provider manager: %w", err)
	}

	providers := providerMgr.List()
	if len(providers) == 0 {
		utils.Info("No providers registered")
		return nil
	}

	for _, p := range providers {
		// Real credential checks require the gdrive/s3 SDKs, which are not wired up yet.
		utils.Warning("%s (%s): live credential check not implemented yet - stored status is '%s'", p.Name, p.Type, p.Status)
	}
	return nil
}

func runProviderReauth(cmd *cobra.Command, args []string) error {
	name := args[0]
	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get provider manager: %w", err)
	}

	if _, err := providerMgr.Get(name); err != nil {
		return err
	}

	return fmt.Errorf("reauth is not implemented yet - OAuth/SDK integration is required first")
}

func runProviderRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	providerMgr, err := provider.GetConfigManager()
	if err != nil {
		return fmt.Errorf("failed to get provider manager: %w", err)
	}

	if _, err := providerMgr.Get(name); err != nil {
		return err
	}

	if !providerForceFlag {
		locMgr, err := location.GetConfigManager()
		if err == nil {
			for _, loc := range locMgr.ListAll() {
				if loc.Provider == name && loc.Active {
					return fmt.Errorf("provider '%s' is referenced by active location '%s' (database '%s') - use --force to remove anyway", name, loc.Name, loc.Database)
				}
			}
		}
	}

	if err := providerMgr.Remove(name); err != nil {
		return err
	}

	secretStore, err := provider.NewSecretStore()
	if err == nil {
		_ = secretStore.Delete(name + ":access_key")
		_ = secretStore.Delete(name + ":secret_key")
	}

	utils.Success("Provider '%s' removed", name)
	return nil
}
