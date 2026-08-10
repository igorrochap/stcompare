package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"stcompare/internal/config"
)

type campaignRunOptions struct {
	configOverrides configOverrideOptions
	force           bool
	keepFailed      bool
}

func newCampaignRunCommand(rootOpts *rootOptions) *cobra.Command {
	options := campaignRunOptions{}
	command := &cobra.Command{
		Use:  "run <campaign>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCampaignRun(cmd, rootOpts, args[0], options)
		},
	}
	addConfigOverrideFlags(command, &options.configOverrides)
	command.Flags().BoolVar(&options.force, "force", false, "")
	command.Flags().BoolVar(&options.keepFailed, "keep-failed", false, "")

	return command
}

func runCampaignRun(cmd *cobra.Command, rootOpts *rootOptions, campaignName string, options campaignRunOptions) error {
	effective, campaign, err := resolveCampaign(cmd, rootOpts.configPath, campaignName, options.configOverrides)
	if err != nil {
		return err
	}

	argv := schemathesisRunCommand(effective, campaignName)
	reportDir := config.CampaignReportDir(effective, campaignName)
	createdReportDir, err := createCampaignReportDir(reportDir, options.force)
	if err != nil {
		return err
	}

	var (
		baselineSchema          []byte
		baselineSchemaAvailable bool
	)
	if campaign.Kind == "baseline" {
		baselineSchema, err = os.ReadFile(effective.Schema)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("snapshot baseline schema: %w", err)
		}
		baselineSchemaAvailable = err == nil
	}

	schemathesisVersion, err := rootOpts.deps.CampaignRunner.SchemathesisVersion()
	if err != nil {
		return fmt.Errorf("get Schemathesis version: %w", err)
	}
	if err := rootOpts.deps.CampaignRunner.Run(argv); err != nil {
		if options.keepFailed {
			if createdReportDir {
				return fmt.Errorf("%w; preserved partial debug artifacts in %s, but this is not a completed campaign", err, reportDir)
			}

			return err
		}

		if createdReportDir {
			_ = os.RemoveAll(reportDir)
		}

		return err
	}

	metadata := campaignRunMetadata{
		Campaign: campaignMetadata{
			Name: campaignName,
			Kind: campaign.Kind,
		},
		ConfigPath:          rootOpts.configPath,
		EffectiveCommand:    argv,
		Settings:            campaignSettingsFromConfig(effective),
		Overrides:           campaignOverrides(cmd, options.configOverrides),
		ToolVersion:         rootOpts.deps.ToolVersion,
		SchemathesisVersion: schemathesisVersion,
		Timestamp:           rootOpts.deps.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	if baselineSchemaAvailable {
		snapshotPath := filepath.Join(reportDir, baselineSchemaSnapshotFilename)
		if err := os.WriteFile(snapshotPath, baselineSchema, 0o644); err != nil {
			return fmt.Errorf("write baseline schema snapshot: %w", err)
		}
		metadata.SchemaSnapshot = snapshotPath
	}
	if err := writeCampaignMetadata(filepath.Join(reportDir, "metadata.yaml"), metadata); err != nil {
		return err
	}

	return nil
}

func createCampaignReportDir(reportDir string, force bool) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(reportDir), 0o755); err != nil {
		return false, fmt.Errorf("create reports directory: %w", err)
	}
	if err := os.Mkdir(reportDir, 0o755); err != nil {
		if os.IsExist(err) {
			if force {
				info, statErr := os.Stat(reportDir)
				if statErr != nil {
					return false, fmt.Errorf("inspect campaign report directory: %w", statErr)
				}
				if !info.IsDir() {
					return false, fmt.Errorf("campaign report path %s exists and is not a directory", reportDir)
				}

				return false, nil
			}

			return false, fmt.Errorf("campaign report directory %s already exists; use --force to overwrite", reportDir)
		}

		return false, fmt.Errorf("create campaign report directory: %w", err)
	}

	return true, nil
}
