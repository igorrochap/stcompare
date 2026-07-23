package cli

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"stcompare/internal/config"
)

func newCampaignCommand(rootOpts *rootOptions) *cobra.Command {
	campaignCommand := &cobra.Command{Use: "campaign"}

	campaignCommand.AddCommand(newCampaignCommandCommand(rootOpts))

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
	effective, err := config.Load(configPath)
	if err != nil {
		return err
	}
	applyConfigOverrides(cmd, &effective, options)
	if err := effective.Validate(); err != nil {
		return err
	}
	if err := validateSchemathesisExtraArgs(effective.Schemathesis.ExtraArgs); err != nil {
		return err
	}
	if err := validateCampaignName(campaignName); err != nil {
		return err
	}
	if _, ok := effective.Campaigns[campaignName]; !ok {
		return fmt.Errorf("campaign %q is not configured", campaignName)
	}

	argv := schemathesisRunCommand(effective, campaignName)
	fmt.Fprintln(cmd.OutOrStdout(), strings.Join(argv, " "))

	return nil
}

func schemathesisRunCommand(effective config.Config, campaignName string) []string {
	reportDir := filepath.Join(effective.ReportsDir, campaignName)

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
	}
	argv = append(argv,
		"--generation-database",
		effective.Schemathesis.GenerationDatabase,
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

var campaignNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validateCampaignName(name string) error {
	if name == "." || name == ".." || !campaignNamePattern.MatchString(name) {
		return fmt.Errorf("campaign name %q is invalid: use letters, numbers, dots, underscores, or hyphens", name)
	}

	return nil
}

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
