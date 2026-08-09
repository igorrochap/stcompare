package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"stcompare/internal/scorecard"
)

type scorecardBuildOptions struct {
	comparisonPath string
	recordPath     string
	outputPath     string
}

func newScorecardCommand() *cobra.Command {
	command := &cobra.Command{Use: "scorecard"}
	command.AddCommand(newScorecardBuildCommand())

	return command
}

func newScorecardBuildCommand() *cobra.Command {
	options := scorecardBuildOptions{}
	command := &cobra.Command{
		Use:  "build",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateScorecardBuildOptions(options); err != nil {
				return toolError(err)
			}
			if err := scorecard.Build(scorecard.Input{
				ComparisonPath: options.comparisonPath,
				RecordPath:     options.recordPath,
				OutputPath:     options.outputPath,
			}); err != nil {
				return toolError(err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", options.outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&options.comparisonPath, "comparison", "", "path to comparison.json")
	command.Flags().StringVar(&options.recordPath, "record", "", "path to benchmark-record.json")
	command.Flags().StringVar(&options.outputPath, "out", "", "path to output HTML")

	return command
}

func validateScorecardBuildOptions(options scorecardBuildOptions) error {
	if options.comparisonPath == "" {
		return errors.New("--comparison is required")
	}
	if options.recordPath == "" {
		return errors.New("--record is required to build a scorecard; use `campaign compare` if you only need the traffic comparison")
	}
	if options.outputPath == "" {
		return errors.New("--out is required")
	}

	return nil
}
