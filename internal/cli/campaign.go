package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"stcompare/internal/config"
)

func newCampaignCommand(rootOpts *rootOptions) *cobra.Command {
	campaignCommand := &cobra.Command{Use: "campaign"}

	campaignCommand.AddCommand(newCampaignCommandCommand(rootOpts))
	campaignCommand.AddCommand(newCampaignCompareCommand(rootOpts))
	campaignCommand.AddCommand(newCampaignRunCommand(rootOpts))

	return campaignCommand
}

func newCampaignCommandCommand(rootOpts *rootOptions) *cobra.Command {
	options := configOverrideOptions{}
	command := &cobra.Command{
		Use:  "command <campaign>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCampaignCommand(cmd, rootOpts.configPath, args[0], options)
		},
	}
	addConfigOverrideFlags(command, &options)

	return command
}

func runCampaignCommand(cmd *cobra.Command, configPath string, campaignName string, options configOverrideOptions) error {
	effective, _, err := resolveCampaign(cmd, configPath, campaignName, options)
	if err != nil {
		return err
	}

	argv := schemathesisRunCommand(effective, campaignName)
	fmt.Fprintln(cmd.OutOrStdout(), strings.Join(argv, " "))

	return nil
}

func resolveCampaign(cmd *cobra.Command, configPath string, campaignName string, options configOverrideOptions) (config.Config, config.Campaign, error) {
	effective, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, config.Campaign{}, err
	}
	applyConfigOverrides(cmd, &effective, options)
	if err := effective.Validate(); err != nil {
		return config.Config{}, config.Campaign{}, err
	}
	if err := validateSchemathesisExtraArgs(effective.Schemathesis.ExtraArgs); err != nil {
		return config.Config{}, config.Campaign{}, err
	}
	if err := config.ValidateCampaignName(campaignName); err != nil {
		return config.Config{}, config.Campaign{}, err
	}
	campaign, ok := effective.Campaigns[campaignName]
	if !ok {
		return config.Config{}, config.Campaign{}, fmt.Errorf("campaign %q is not configured", campaignName)
	}

	return effective, campaign, nil
}

func schemathesisRunCommand(effective config.Config, campaignName string) []string {
	reportDir := campaignReportDir(effective, campaignName)

	argv := []string{
		"st",
		"run",
		effective.Schema,
		"--url",
		effective.BaseURL,
		"--workers",
		strconv.Itoa(effective.Schemathesis.Workers),
		"--seed",
		strconv.Itoa(effective.Schemathesis.Seed),
	}
	if effective.Schemathesis.GenerationDeterministic {
		argv = append(argv, "--generation-deterministic")
	} else {
		argv = append(argv,
			"--generation-database",
			effective.Schemathesis.GenerationDatabase,
		)
	}
	argv = append(argv,
		"--report",
		strings.Join(effective.Schemathesis.Reports, ","),
		"--report-junit-path",
		filepath.Join(reportDir, "junit.xml"),
		"--report-vcr-path",
		filepath.Join(reportDir, "campaign.vcr.yaml"),
		"--report-har-path",
		filepath.Join(reportDir, "campaign.har.json"),
		"--report-ndjson-path",
		filepath.Join(reportDir, "campaign.ndjson"),
		"--output-sanitize",
		strconv.FormatBool(effective.Schemathesis.OutputSanitize),
		"--output-truncate",
		strconv.FormatBool(effective.Schemathesis.OutputTruncate),
	)
	argv = append(argv, effective.Schemathesis.ExtraArgs...)

	return argv
}

func campaignReportDir(effective config.Config, campaignName string) string {
	return filepath.Join(effective.ReportsDir, campaignName)
}

const baselineSchemaSnapshotFilename = "schema.snapshot"

func validateSchemathesisExtraArgs(extraArgs []string) error {
	toolOwnedReportOptions := []string{
		"--report",
		"--report-junit-path",
		"--report-vcr-path",
		"--report-har-path",
		"--report-ndjson-path",
	}
	for _, arg := range extraArgs {
		for _, option := range toolOwnedReportOptions {
			if arg == option || strings.HasPrefix(arg, option+"=") {
				return fmt.Errorf("schemathesis.extra_args cannot override tool-owned report option %q", option)
			}
		}
	}

	return nil
}
