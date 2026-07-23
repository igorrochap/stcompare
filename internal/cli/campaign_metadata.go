package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"stcompare/internal/config"
)

type campaignRunMetadata struct {
	Campaign            campaignMetadata `yaml:"campaign"`
	ConfigPath          string           `yaml:"config_path"`
	EffectiveCommand    []string         `yaml:"effective_command"`
	Settings            campaignSettings `yaml:"settings"`
	Overrides           map[string]any   `yaml:"overrides"`
	ToolVersion         string           `yaml:"tool_version"`
	SchemathesisVersion string           `yaml:"schemathesis_version"`
	Timestamp           string           `yaml:"timestamp"`
}

type campaignMetadata struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
}

type campaignSettings struct {
	Schema                  string   `yaml:"schema"`
	BaseURL                 string   `yaml:"base_url"`
	ReportsDir              string   `yaml:"reports_dir"`
	Seed                    int      `yaml:"seed"`
	Workers                 int      `yaml:"workers"`
	GenerationDeterministic bool     `yaml:"generation_deterministic"`
	GenerationDatabase      string   `yaml:"generation_database"`
	Reports                 []string `yaml:"reports"`
	OutputSanitize          bool     `yaml:"output_sanitize"`
	OutputTruncate          bool     `yaml:"output_truncate"`
	ExtraArgs               []string `yaml:"extra_args"`
}

func campaignSettingsFromConfig(effective config.Config) campaignSettings {
	return campaignSettings{
		Schema:                  effective.Schema,
		BaseURL:                 effective.BaseURL,
		ReportsDir:              effective.ReportsDir,
		Seed:                    effective.Schemathesis.Seed,
		Workers:                 effective.Schemathesis.Workers,
		GenerationDeterministic: effective.Schemathesis.GenerationDeterministic,
		GenerationDatabase:      effective.Schemathesis.GenerationDatabase,
		Reports:                 effective.Schemathesis.Reports,
		OutputSanitize:          effective.Schemathesis.OutputSanitize,
		OutputTruncate:          effective.Schemathesis.OutputTruncate,
		ExtraArgs:               effective.Schemathesis.ExtraArgs,
	}
}

func campaignOverrides(cmd *cobra.Command, options configOverrideOptions) map[string]any {
	overrides := map[string]any{}
	if cmd.Flags().Changed("schema") {
		overrides["schema"] = options.schema
	}
	if cmd.Flags().Changed("base-url") {
		overrides["base_url"] = options.baseURL
	}
	if cmd.Flags().Changed("reports-dir") {
		overrides["reports_dir"] = options.reportsDir
	}
	if cmd.Flags().Changed("seed") {
		overrides["seed"] = options.seed
	}
	if cmd.Flags().Changed("workers") {
		overrides["workers"] = options.workers
	}

	return overrides
}

func writeCampaignMetadata(path string, metadata campaignRunMetadata) error {
	contents, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal campaign metadata: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write campaign metadata: %w", err)
	}

	return nil
}
