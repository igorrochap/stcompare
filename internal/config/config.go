package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultFilename = "stcompare.yaml"

type Config struct {
	Schema       string              `yaml:"schema"`
	BaseURL      string              `yaml:"base_url"`
	ReportsDir   string              `yaml:"reports_dir"`
	Schemathesis SchemathesisConfig  `yaml:"schemathesis"`
	Comparison   ComparisonConfig    `yaml:"comparison,omitempty"`
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

type ComparisonConfig struct {
	MissingResourceStatuses []int                   `yaml:"missing_resource_statuses"`
	PreconditionHeuristics  []PreconditionHeuristic `yaml:"precondition_heuristics"`
	Normalization           NormalizationConfig     `yaml:"normalization"`
}

type PreconditionHeuristic struct {
	Name        string `yaml:"name"`
	Method      string `yaml:"method"`
	PathPattern string `yaml:"path_pattern"`
}

type NormalizationConfig struct {
	DefaultRules bool                         `yaml:"default_rules"`
	BodyFields   []BodyFieldNormalizationRule `yaml:"body_fields"`
	Headers      []HeaderNormalizationRule    `yaml:"headers"`
}

type BodyFieldNormalizationRule struct {
	Name      string `yaml:"name"`
	FieldName string `yaml:"field_name"`
}

type HeaderNormalizationRule struct {
	Name       string `yaml:"name"`
	HeaderName string `yaml:"header_name"`
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
	for index, status := range c.Comparison.MissingResourceStatuses {
		if !isAllowedMissingResourceStatus(status) {
			return fmt.Errorf(
				"comparison.missing_resource_statuses[%d] must be one of 401, 403, 404, or 410",
				index,
			)
		}
	}
	heuristicNames := make(map[string]struct{}, len(c.Comparison.PreconditionHeuristics))
	for index, heuristic := range c.Comparison.PreconditionHeuristics {
		name := strings.TrimSpace(heuristic.Name)
		if name == "" {
			return fmt.Errorf(
				"comparison.precondition_heuristics[%d].name is required",
				index,
			)
		}
		if _, exists := heuristicNames[name]; exists {
			return fmt.Errorf(
				"comparison.precondition_heuristics[%d].name must be unique",
				index,
			)
		}
		heuristicNames[name] = struct{}{}
		if strings.TrimSpace(heuristic.Method) == "" {
			return fmt.Errorf(
				"comparison.precondition_heuristics[%d].method is required",
				index,
			)
		}
		if strings.TrimSpace(heuristic.PathPattern) == "" {
			return fmt.Errorf(
				"comparison.precondition_heuristics[%d].path_pattern is required",
				index,
			)
		}
		if _, err := regexp.Compile(heuristic.PathPattern); err != nil {
			return fmt.Errorf(
				"comparison.precondition_heuristics[%d].path_pattern must be a valid regular expression",
				index,
			)
		}
	}
	for index, rule := range c.Comparison.Normalization.BodyFields {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf(
				"comparison.normalization.body_fields[%d].name is required",
				index,
			)
		}
		if strings.TrimSpace(rule.FieldName) == "" {
			return fmt.Errorf(
				"comparison.normalization.body_fields[%d].field_name is required",
				index,
			)
		}
	}
	for index, rule := range c.Comparison.Normalization.Headers {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf(
				"comparison.normalization.headers[%d].name is required",
				index,
			)
		}
		if strings.TrimSpace(rule.HeaderName) == "" {
			return fmt.Errorf(
				"comparison.normalization.headers[%d].header_name is required",
				index,
			)
		}
	}
	if len(c.Campaigns) == 0 {
		return errors.New("at least one campaign is required")
	}
	for name, campaign := range c.Campaigns {
		if err := ValidateCampaignName(name); err != nil {
			return err
		}
		switch campaign.Kind {
		case "baseline", "candidate":
		default:
			return fmt.Errorf("campaign %q has invalid kind %q: must be baseline or candidate", name, campaign.Kind)
		}
	}

	return nil
}

func isAllowedMissingResourceStatus(status int) bool {
	switch status {
	case 401, 403, 404, 410:
		return true
	}

	return false
}

var campaignNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ValidateCampaignName(name string) error {
	if name == "." || name == ".." || !campaignNamePattern.MatchString(name) {
		return fmt.Errorf("campaign name %q is invalid: use letters, numbers, dots, underscores, or hyphens", name)
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
		Comparison: ComparisonConfig{
			MissingResourceStatuses: []int{404, 410},
			PreconditionHeuristics:  []PreconditionHeuristic{},
			Normalization: NormalizationConfig{
				DefaultRules: true,
				BodyFields:   []BodyFieldNormalizationRule{},
				Headers:      []HeaderNormalizationRule{},
			},
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

	defaults := Default()
	loaded := Config{
		Schemathesis: defaults.Schemathesis,
		Comparison:   defaults.Comparison,
	}
	if err := yaml.Unmarshal(contents, &loaded); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}

	return loaded, nil
}
