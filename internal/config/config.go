package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultFilename = "stcompare.yaml"

type Config struct {
	Schema       string              `yaml:"schema"`
	BaseURL      string              `yaml:"base_url"`
	ReportsDir   string              `yaml:"reports_dir"`
	Schemathesis SchemathesisConfig  `yaml:"schemathesis"`
	Campaigns    map[string]Campaign `yaml:"campaigns"`
}

type SchemathesisConfig struct {
	Seed                    int      `yaml:"seed"`
	Workers                 int      `yaml:"workers"`
	GenerationDeterministic bool     `yaml:"generation_deterministic"`
	GenerationDatabase      string   `yaml:"generation_database"`
	Reports                 []string `yaml:"reports"`
	OutputSanitize          bool     `yaml:"output_sanitize"`
	OutputTruncate          bool     `yaml:"output_truncate"`
	ExtraArgs               []string `yaml:"extra_args"`
}

type Campaign struct {
	Kind string `yaml:"kind"`
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Schema) == "" {
		return errors.New("schema is required")
	}

	return nil
}

func Default() Config {
	return Config{
		Schema:     "openapi.json",
		BaseURL:    "http://localhost:8080",
		ReportsDir: "reports",
		Schemathesis: SchemathesisConfig{
			Seed:                    12345,
			Workers:                 1,
			GenerationDeterministic: true,
			GenerationDatabase:      "none",
			Reports:                 []string{"junit", "vcr", "har", "ndjson"},
			ExtraArgs:               []string{},
		},
		Campaigns: map[string]Campaign{
			"baseline": {Kind: "baseline"},
			"gpt5.6":   {Kind: "candidate"},
			"sonnet5":  {Kind: "candidate"},
		},
	}
}

func WriteDefault(path string) error {
	contents, err := yaml.Marshal(Default())
	if err != nil {
		return fmt.Errorf("marshal default config: %w", err)
	}

	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

func Load(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	var loaded Config
	if err := yaml.Unmarshal(contents, &loaded); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}

	return loaded, nil
}
