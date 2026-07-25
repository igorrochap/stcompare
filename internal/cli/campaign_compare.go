package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"stcompare/internal/comparison"
)

type campaignCompareOptions struct {
	configOverrides configOverrideOptions
}

func newCampaignCompareCommand(rootOpts *rootOptions) *cobra.Command {
	options := campaignCompareOptions{}
	command := &cobra.Command{
		Use:  "compare <candidate>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCampaignCompare(cmd, rootOpts, args[0], options)
		},
	}
	addConfigOverrideFlags(command, &options.configOverrides)

	return command
}

func runCampaignCompare(
	cmd *cobra.Command,
	rootOpts *rootOptions,
	candidateName string,
	options campaignCompareOptions,
) error {
	effective, candidate, err := resolveCampaign(
		cmd,
		rootOpts.configPath,
		candidateName,
		options.configOverrides,
	)
	if err != nil {
		return err
	}
	if candidate.Kind != "candidate" {
		return fmt.Errorf(
			"campaign %q has kind %q: compare requires a candidate campaign",
			candidateName,
			candidate.Kind,
		)
	}

	baselineName := ""
	baselineCount := 0
	for name, campaign := range effective.Campaigns {
		if campaign.Kind == "baseline" {
			baselineName = name
			baselineCount++
		}
	}
	if baselineCount != 1 {
		return fmt.Errorf(
			"baseline replay setup: expected exactly one baseline campaign, found %d",
			baselineCount,
		)
	}

	result, err := comparison.Compare(
		comparison.Input{
			BaselineCampaign: baselineName,
			BaselineHARPath: filepath.Join(
				effective.ReportsDir,
				baselineName,
				"campaign.har.json",
			),
			BaselineJUnitPath: filepath.Join(
				effective.ReportsDir,
				baselineName,
				"junit.xml",
			),
			CandidateCampaign: candidateName,
			CandidateBaseURL:  effective.BaseURL,
			OutputDir:         filepath.Join(effective.ReportsDir, candidateName),
		},
		comparison.Dependencies{Now: rootOpts.deps.Now},
	)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "replayed %d baseline interactions\n", result.InteractionCount)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", result.ReplayLogPath)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", result.JSONReportPath)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", result.MarkdownReportPath)

	return nil
}
