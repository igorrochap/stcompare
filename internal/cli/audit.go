package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"stcompare/internal/audit"
)

type auditRenderOptions struct {
	auditPath  string
	outputPath string
}

func newAuditCommand() *cobra.Command {
	command := &cobra.Command{Use: "audit"}
	options := auditRenderOptions{}
	render := &cobra.Command{
		Use:  "render",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := validateAuditRenderOptions(options); err != nil {
				return err
			}
			return audit.Build(options.auditPath, options.outputPath)
		},
	}
	render.Flags().StringVar(&options.auditPath, "audit", "", "path to the benchmark audit artifact")
	render.Flags().StringVar(&options.outputPath, "out", "", "path for the chronological HTML audit report")
	command.AddCommand(render)
	return command
}

func validateAuditRenderOptions(options auditRenderOptions) error {
	if options.auditPath == "" {
		return errors.New("--audit is required to render an audit")
	}
	if options.outputPath == "" {
		return errors.New("--out is required to render an audit")
	}
	return nil
}
