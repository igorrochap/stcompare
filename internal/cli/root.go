package cli

import (
	"time"

	"github.com/spf13/cobra"

	"stcompare/internal/config"
)

type rootOptions struct {
	configPath string
	deps       Dependencies
}

type Dependencies struct {
	CampaignRunner CampaignRunner
	Now            func() time.Time
	ToolVersion    string
}

type CampaignRunner interface {
	SchemathesisVersion() (string, error)
	Run(argv []string) error
}

func NewRootCommand() *cobra.Command {
	return NewRootCommandWithDependencies(Dependencies{})
}

func NewRootCommandWithDependencies(deps Dependencies) *cobra.Command {
	if deps.CampaignRunner == nil {
		deps.CampaignRunner = execCampaignRunner{}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.ToolVersion == "" {
		deps.ToolVersion = "dev"
	}

	options := rootOptions{deps: deps}
	root := &cobra.Command{
		Use:          "stcompare",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&options.configPath, "config", config.DefaultFilename, "")
	root.AddCommand(newConfigCommand(&options))
	root.AddCommand(newCampaignCommand(&options))
	root.AddCommand(newScorecardCommand())

	return root
}
