package cli

import (
	"github.com/spf13/cobra"

	"stcompare/internal/config"
)

type rootOptions struct {
	configPath string
}

func NewRootCommand() *cobra.Command {
	options := rootOptions{}
	root := &cobra.Command{
		Use:          "stcompare",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&options.configPath, "config", config.DefaultFilename, "")
	root.AddCommand(newConfigCommand(&options))
	root.AddCommand(newCampaignCommand(&options))

	return root
}
