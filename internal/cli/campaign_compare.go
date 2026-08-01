package cli

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"stcompare/agentreport"
	"stcompare/internal/comparison"
	"stcompare/internal/config"
)

type campaignCompareOptions struct {
	configOverrides configOverrideOptions
}

// ExitCodeError carries the process exit code selected by a CLI command.
type ExitCodeError struct {
	// Code is the process exit code selected by the command.
	Code int
	// Err is the underlying command failure, when one exists.
	Err error
}

func (e *ExitCodeError) Error() string {
	return e.Err.Error()
}

func (e *ExitCodeError) Unwrap() error {
	return e.Err
}

func exitCodeError(code int, err error) *ExitCodeError {
	return &ExitCodeError{Code: code, Err: err}
}

func toolError(err error) error {
	if err == nil {
		return nil
	}
	return exitCodeError(agentreport.ExitCodeToolError, err)
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
		return toolError(err)
	}
	if err := requireCandidateCampaign(candidateName, candidate); err != nil {
		return toolError(err)
	}

	baselineName := baselineCampaignName(effective)

	result, err := comparison.Compare(
		campaignComparisonInput(effective, baselineName, candidateName),
		comparison.Dependencies{Now: rootOpts.deps.Now},
	)
	if err != nil {
		return toolError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "replayed %d baseline interactions\n", result.InteractionCount)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", result.ReplayLogPath)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", result.JSONReportPath)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", result.MarkdownReportPath)
	htmlReportURI, err := fileURI(result.HTMLReportPath)
	if err != nil {
		return toolError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", htmlReportURI)

	if result.Converged {
		return nil
	}

	return exitCodeError(
		agentreport.ExitCodeNotConverged,
		errors.New("comparison did not converge"),
	)
}

func fileURI(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve HTML report path: %w", err)
	}

	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}).String(), nil
}

func requireCandidateCampaign(candidateName string, campaign config.Campaign) error {
	if campaign.Kind != "candidate" {
		return fmt.Errorf(
			"campaign %q has kind %q: compare requires a candidate campaign",
			candidateName,
			campaign.Kind,
		)
	}

	return nil
}

func baselineCampaignName(effective config.Config) string {
	for name, campaign := range effective.Campaigns {
		if campaign.Kind == "baseline" {
			return name
		}
	}

	panic("validated config has no baseline campaign")
}

func campaignComparisonInput(
	effective config.Config,
	baselineName string,
	candidateName string,
) comparison.Input {
	baselineReportDir := campaignReportDir(effective, baselineName)
	candidateReportDir := campaignReportDir(effective, candidateName)

	return comparison.Input{
		BaselineCampaign:   baselineName,
		SchemaPath:         effective.Schema,
		BaselineHARPath:    filepath.Join(baselineReportDir, "campaign.har.json"),
		BaselineVCRPath:    filepath.Join(baselineReportDir, "campaign.vcr.yaml"),
		BaselineNDJSONPath: filepath.Join(baselineReportDir, "campaign.ndjson"),
		BaselineJUnitPath:  filepath.Join(baselineReportDir, "junit.xml"),
		CandidateCampaign:  candidateName,
		CandidateBaseURL:   effective.BaseURL,
		OutputDir:          candidateReportDir,
		PreconditionPolicy: campaignPreconditionPolicy(effective.Comparison),
	}
}

func campaignPreconditionPolicy(source config.ComparisonConfig) comparison.PreconditionPolicy {
	policy := comparison.PreconditionPolicy{
		MissingResourceStatuses: make([]int, len(source.MissingResourceStatuses)),
		Heuristics: make(
			[]comparison.PreconditionHeuristic,
			len(source.PreconditionHeuristics),
		),
		Normalization: comparison.NormalizationConfigFrom(source.Normalization),
	}
	copy(policy.MissingResourceStatuses, source.MissingResourceStatuses)
	for index, heuristic := range source.PreconditionHeuristics {
		policy.Heuristics[index] = comparison.NewPreconditionHeuristic(
			strings.TrimSpace(heuristic.Name),
			strings.TrimSpace(heuristic.Method),
			heuristic.PathPattern,
		)
	}

	return policy
}
