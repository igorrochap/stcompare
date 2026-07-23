package config

import (
	"errors"
	"fmt"
	"net/url"
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
	baseURL, err := url.Parse(c.BaseURL)
	if err != nil || !baseURL.IsAbs() || baseURL.Host == "" ||
		(!strings.EqualFold(baseURL.Scheme, "http") && !strings.EqualFold(baseURL.Scheme, "https")) {
		return errors.New("base_url must be an absolute HTTP(S) URL")
	}
	if strings.TrimSpace(c.ReportsDir) == "" {
		return errors.New("reports_dir is required")
	}
	if c.Schemathesis.Workers < 1 {
		return errors.New("schemathesis.workers must be at least 1")
	}
	if len(c.Campaigns) == 0 {
		return errors.New("at least one campaign is required")
	}
	for name, campaign := range c.Campaigns {
		switch campaign.Kind {
		case "baseline", "candidate":
		default:
			return fmt.Errorf("campaign %q has invalid kind %q: must be baseline or candidate", name, campaign.Kind)
		}
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

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", path)
	}
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()

	if _, err := file.Write(contents); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	committed = true

	return nil
}

func OverwriteDefault(path string) error {
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

	loaded := Config{Schemathesis: Default().Schemathesis}
	if err := yaml.Unmarshal(contents, &loaded); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}

	return loaded, nil
}
