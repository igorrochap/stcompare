package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultFilename = "stcompare.yaml"

// CampaignReportDir returns the configured report directory for a campaign.
func CampaignReportDir(effective Config, campaignName string) string {
	return filepath.Join(effective.ReportsDir, campaignName)
}

type Config struct {
	Schema        string              `yaml:"schema"`
	CandidateSpec string              `yaml:"candidate_spec,omitempty"`
	BaseURL       string              `yaml:"base_url"`
	ReportsDir    string              `yaml:"reports_dir"`
	Schemathesis  SchemathesisConfig  `yaml:"schemathesis"`
	Comparison    ComparisonConfig    `yaml:"comparison,omitempty"`
	Campaigns     map[string]Campaign `yaml:"campaigns"`
	Stbench       *StbenchConfig      `yaml:"stbench,omitempty"`
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
	Kind    string `yaml:"kind"`
	Agent   string `yaml:"agent,omitempty"`
	Model   string `yaml:"model,omitempty"`
	Effort  string `yaml:"effort,omitempty"`
	Adapter string `yaml:"adapter,omitempty"`
}

// StbenchConfig contains optional settings for the benchmark runner.
type StbenchConfig struct {
	Hardware        string                 `yaml:"hardware"`
	Adapters        map[string]string      `yaml:"adapters"`
	AdapterTimeout  string                 `yaml:"adapter_timeout"`
	ReuseProcess    bool                   `yaml:"reuse_process"`
	SourceDir       string                 `yaml:"source_dir"`
	StcompareBinary string                 `yaml:"stcompare_binary"`
	Prompt          StbenchPromptConfig    `yaml:"prompt,omitempty"`
	Lifecycle       StbenchLifecycleConfig `yaml:"lifecycle,omitempty"`
	MaxIterations   int                    `yaml:"max_iterations"`
	StallWindow     int                    `yaml:"stall_window"`
}

// UnmarshalYAML rejects the old candidate names with an actionable migration
// message instead of silently ignoring them as unknown YAML fields.
func (c *StbenchConfig) UnmarshalYAML(node *yaml.Node) error {
	type stbenchConfig StbenchConfig
	var decoded stbenchConfig
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		switch node.Content[index].Value {
		case "candidate":
			return errors.New("stbench.candidate is deprecated; use stbench.campaign")
		case "candidate_dir":
			return errors.New("stbench.candidate_dir is deprecated; use stbench.source_dir")
		}
	}

	*c = StbenchConfig(decoded)
	return nil
}

// StbenchPromptConfig identifies the fixed task prompt used by a run.
type StbenchPromptConfig struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version"`
}

// StbenchLifecycleConfig contains candidate process and health-check hooks.
type StbenchLifecycleConfig struct {
	Stop           string `yaml:"stop"`
	Reset          string `yaml:"reset"`
	Build          string `yaml:"build"`
	Start          string `yaml:"start"`
	CommandTimeout string `yaml:"command_timeout"`
	HealthURL      string `yaml:"health_url"`
	HealthTimeout  string `yaml:"health_timeout"`
	HealthInterval string `yaml:"health_interval"`
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Schema) == "" {
		return errors.New("schema is required")
	}
	baseURL, err := parseHTTPURL(c.BaseURL)
	if err != nil {
		return errors.New("base_url must be an absolute HTTP(S) URL")
	}
	if c.Stbench != nil {
		if err := validateStbenchHealthURL(baseURL, c.Stbench.Lifecycle.HealthURL); err != nil {
			return err
		}
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
	baselineCount := 0
	for name, campaign := range c.Campaigns {
		if err := ValidateCampaignName(name); err != nil {
			return err
		}
		switch campaign.Kind {
		case "baseline":
			baselineCount++
		case "candidate":
		default:
			return fmt.Errorf("campaign %q has invalid kind %q: must be baseline or candidate", name, campaign.Kind)
		}
	}
	switch baselineCount {
	case 0:
		return errors.New("exactly one baseline campaign is required: found none")
	case 1:
	default:
		return fmt.Errorf("exactly one baseline campaign is required: found %d", baselineCount)
	}

	return nil
}

func parseHTTPURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return nil, errors.New("URL must be an absolute HTTP(S) URL")
	}

	return parsed, nil
}

func validateStbenchHealthURL(baseURL *url.URL, rawHealthURL string) error {
	if strings.TrimSpace(rawHealthURL) == "" {
		return nil
	}

	healthURL, err := parseHTTPURL(rawHealthURL)
	if err != nil {
		return errors.New("stbench.lifecycle.health_url must be an absolute HTTP(S) URL")
	}

	baseHostPort := normalizedHostPort(baseURL)
	healthHostPort := normalizedHostPort(healthURL)
	if baseHostPort != healthHostPort {
		return fmt.Errorf(
			"stbench.lifecycle.health_url host/port must match base_url: got %q, want %q",
			healthHostPort,
			baseHostPort,
		)
	}

	return nil
}

func normalizedHostPort(parsed *url.URL) string {
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}

	return net.JoinHostPort(host, port)
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
