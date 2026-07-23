package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"stcompare/internal/config"
)

func newConfigCommand() *cobra.Command {
	configCommand := &cobra.Command{Use: "config"}

	configCommand.AddCommand(newConfigInitCommand())
	configCommand.AddCommand(newConfigShowCommand())

	return configCommand
}

func newConfigInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "init",
		RunE: runConfigInit,
	}
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	return config.WriteDefault(config.DefaultFilename)
}

type configShowOptions struct {
	schema     string
	baseURL    string
	reportsDir string
	seed       int
	workers    int
}

func newConfigShowCommand() *cobra.Command {
	options := configShowOptions{}
	command := &cobra.Command{
		Use: "show",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigShow(cmd, options)
		},
	}
	command.Flags().StringVar(&options.schema, "schema", "", "")
	command.Flags().StringVar(&options.baseURL, "base-url", "", "")
	command.Flags().StringVar(&options.reportsDir, "reports-dir", "", "")
	command.Flags().IntVar(&options.seed, "seed", 0, "")
	command.Flags().IntVar(&options.workers, "workers", 0, "")

	return command
}

func runConfigShow(cmd *cobra.Command, options configShowOptions) error {
	effective, err := config.Load(config.DefaultFilename)
	if err != nil {
		return err
	}

	if cmd.Flags().Changed("schema") {
		effective.Schema = options.schema
	}
	if cmd.Flags().Changed("base-url") {
		effective.BaseURL = options.baseURL
	}
	if cmd.Flags().Changed("reports-dir") {
		effective.ReportsDir = options.reportsDir
	}
	if cmd.Flags().Changed("seed") {
		effective.Schemathesis.Seed = options.seed
	}
	if cmd.Flags().Changed("workers") {
		effective.Schemathesis.Workers = options.workers
	}

	if err := yaml.NewEncoder(cmd.OutOrStdout()).Encode(effective); err != nil {
		return fmt.Errorf("encode effective config: %w", err)
	}

	return nil
}
