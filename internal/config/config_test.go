package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validCandidateIdentityConfig() Config {
	config := Default()
	config.Campaigns = map[string]Campaign{
		"baseline": {Kind: "baseline"},
		"sonnet5-high": {
			Kind:    "candidate",
			Agent:   "claude-code",
			Model:   "sonnet-5",
			Effort:  "high",
			Adapter: "remote",
		},
	}
	config.Stbench = &StbenchConfig{
		Adapters: map[string]string{
			"remote": "python adapters/anthropic_adapter.py",
		},
	}

	return config
}

func validCandidateCampaign() Campaign {
	return Campaign{
		Kind:    "candidate",
		Agent:   "claude-code",
		Model:   "sonnet-5",
		Effort:  "high",
		Adapter: "remote",
	}
}

func TestCampaignReportDirJoinsConfiguredRootAndCampaign(t *testing.T) {
	effective := Config{ReportsDir: filepath.Join("custom", "reports")}

	got := CampaignReportDir(effective, "candidate")
	want := filepath.Join("custom", "reports", "candidate")
	if got != want {
		t.Fatalf("CampaignReportDir() = %q, want %q", got, want)
	}
}

func TestLoadParsesOptionalStbenchConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stcompare.yaml")
	contents := `schema: openapi.json
base_url: http://localhost:8080
reports_dir: reports
schemathesis:
  workers: 1
campaigns:
  baseline:
    kind: baseline
  sonnet5-high:
    kind: candidate
    agent: claude-code
    model: sonnet-5
    effort: high
    adapter: remote
stbench:
  hardware: RTX 4090 / 64GB
  reuse_process: true
  source_dir: candidate-src
  adapters:
    remote: python adapters/anthropic_adapter.py
  adapter_timeout: 3m
  lifecycle:
    stop: ./stop.sh
    build: ./build.sh
    start: ./start.sh
    command_timeout: 2m
    health_url: http://localhost:8080/health
    health_timeout: 5s
candidate_spec: /openapi.json
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Stbench == nil {
		t.Fatal("Stbench = nil, want parsed stbench section")
	}
	if !loaded.Stbench.ReuseProcess || loaded.Stbench.SourceDir != "candidate-src" ||
		loaded.Stbench.Hardware != "RTX 4090 / 64GB" ||
		loaded.Stbench.Adapters["remote"] != "python adapters/anthropic_adapter.py" ||
		loaded.Stbench.AdapterTimeout != "3m" || loaded.Stbench.Lifecycle.CommandTimeout != "2m" {
		t.Fatalf("Stbench = %#v, want fixed benchmark infrastructure", loaded.Stbench)
	}
	candidate := loaded.Campaigns["sonnet5-high"]
	if candidate.Agent != "claude-code" || candidate.Model != "sonnet-5" ||
		candidate.Effort != "high" || candidate.Adapter != "remote" {
		t.Fatalf("candidate = %#v, want campaign identity", candidate)
	}
	if loaded.Stbench.Lifecycle.HealthTimeout != "5s" {
		t.Fatalf("health timeout = %q, want %q", loaded.Stbench.Lifecycle.HealthTimeout, "5s")
	}
	if loaded.CandidateSpec != "/openapi.json" {
		t.Fatalf("candidate spec = %q, want %q", loaded.CandidateSpec, "/openapi.json")
	}
}

func TestLoadRejectsUnknownCandidateAdapter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stcompare.yaml")
	contents := `schema: openapi.json
base_url: http://localhost:8080
reports_dir: reports
schemathesis:
  workers: 1
campaigns:
  baseline:
    kind: baseline
  sonnet5-high:
    kind: candidate
    agent: claude-code
    model: sonnet-5
    effort: high
    adapter: remot
stbench:
  adapters:
    remote: python adapters/anthropic_adapter.py
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown-adapter error")
	}
	want := `campaign "sonnet5-high": adapter "remot" is not defined in stbench.adapters`
	if err.Error() != want {
		t.Fatalf("Load() error = %q, want %q", err.Error(), want)
	}
}

func TestLoadRejectsDeprecatedStbenchCandidateNames(t *testing.T) {
	for _, test := range []struct {
		name        string
		key         string
		wantMessage string
	}{
		{
			name:        "campaign",
			key:         "candidate",
			wantMessage: "stbench.candidate is deprecated; use stbench.campaign",
		},
		{
			name:        "source directory",
			key:         "candidate_dir",
			wantMessage: "stbench.candidate_dir is deprecated; use stbench.source_dir",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "CONFIG")
			contents := "stbench:\n  " + test.key + ": value\n"
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want deprecated-name error")
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("Load() error = %q, want message %q", err.Error(), test.wantMessage)
			}
		})
	}
}

func TestConfigValidateRejectsTrimmedDuplicatePreconditionHeuristicNames(t *testing.T) {
	config := validCandidateIdentityConfig()
	config.Comparison.PreconditionHeuristics = []PreconditionHeuristic{
		{
			Name:        "generated-widget",
			Method:      "GET",
			PathPattern: `^/widgets/[0-9a-f]+$`,
		},
		{
			Name:        " generated-widget ",
			Method:      "POST",
			PathPattern: `^/widgets$`,
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want duplicate-name error")
	}

	want := "comparison.precondition_heuristics[1].name must be unique"
	if err.Error() != want {
		t.Fatalf("Validate() error = %q, want %q", err.Error(), want)
	}
}

func TestConfigValidateRejectsMalformedNormalizationRules(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantError string
	}{
		{
			name: "blank body rule name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.BodyFields = []BodyFieldNormalizationRule{
					{Name: " ", FieldName: "id"},
				}
			},
			wantError: "comparison.normalization.body_fields[0].name is required",
		},
		{
			name: "blank body field name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.BodyFields = []BodyFieldNormalizationRule{
					{Name: "generated-id", FieldName: " "},
				}
			},
			wantError: "comparison.normalization.body_fields[0].field_name is required",
		},
		{
			name: "blank header rule name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.Headers = []HeaderNormalizationRule{
					{Name: " ", HeaderName: "date"},
				}
			},
			wantError: "comparison.normalization.headers[0].name is required",
		},
		{
			name: "blank header name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.Headers = []HeaderNormalizationRule{
					{Name: "date-header", HeaderName: " "},
				}
			},
			wantError: "comparison.normalization.headers[0].header_name is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validCandidateIdentityConfig()
			test.mutate(&config)

			err := config.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want normalization rule error")
			}
			if err.Error() != test.wantError {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), test.wantError)
			}
		})
	}
}

func TestConfigValidateRequiresExactlyOneBaselineCampaign(t *testing.T) {
	tests := []struct {
		name      string
		campaigns map[string]Campaign
		wantError string
	}{
		{
			name: "zero baseline campaigns",
			campaigns: map[string]Campaign{
				"gpt5.6":  validCandidateCampaign(),
				"sonnet5": validCandidateCampaign(),
			},
			wantError: "exactly one baseline campaign is required: found none",
		},
		{
			name: "one baseline campaign",
			campaigns: map[string]Campaign{
				"reference": {Kind: "baseline"},
				"gpt5.6":    validCandidateCampaign(),
			},
		},
		{
			name: "multiple baseline campaigns",
			campaigns: map[string]Campaign{
				"reference": {Kind: "baseline"},
				"control":   {Kind: "baseline"},
				"gpt5.6":    validCandidateCampaign(),
			},
			wantError: "exactly one baseline campaign is required: found 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validCandidateIdentityConfig()
			config.Campaigns = test.campaigns

			err := config.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				return
			}

			if err == nil {
				t.Fatal("Validate() error = nil, want baseline-count error")
			}
			if err.Error() != test.wantError {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), test.wantError)
			}
		})
	}
}

func TestConfigValidateEnforcesCampaignIdentity(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantError string
	}{
		{
			name: "candidate requires agent",
			mutate: func(config *Config) {
				campaign := config.Campaigns["sonnet5-high"]
				campaign.Agent = " "
				config.Campaigns["sonnet5-high"] = campaign
			},
			wantError: `campaign "sonnet5-high": agent is required for candidate campaigns`,
		},
		{
			name: "candidate requires model",
			mutate: func(config *Config) {
				campaign := config.Campaigns["sonnet5-high"]
				campaign.Model = ""
				config.Campaigns["sonnet5-high"] = campaign
			},
			wantError: `campaign "sonnet5-high": model is required for candidate campaigns`,
		},
		{
			name: "candidate requires effort",
			mutate: func(config *Config) {
				campaign := config.Campaigns["sonnet5-high"]
				campaign.Effort = "\t"
				config.Campaigns["sonnet5-high"] = campaign
			},
			wantError: `campaign "sonnet5-high": effort is required for candidate campaigns`,
		},
		{
			name: "candidate requires adapter",
			mutate: func(config *Config) {
				campaign := config.Campaigns["sonnet5-high"]
				campaign.Adapter = ""
				config.Campaigns["sonnet5-high"] = campaign
			},
			wantError: `campaign "sonnet5-high": adapter is required for candidate campaigns`,
		},
		{
			name: "candidate adapter must resolve",
			mutate: func(config *Config) {
				campaign := config.Campaigns["sonnet5-high"]
				campaign.Adapter = "remot"
				config.Campaigns["sonnet5-high"] = campaign
			},
			wantError: `campaign "sonnet5-high": adapter "remot" is not defined in stbench.adapters`,
		},
		{
			name: "baseline forbids agent",
			mutate: func(config *Config) {
				campaign := config.Campaigns["baseline"]
				campaign.Agent = "claude-code"
				config.Campaigns["baseline"] = campaign
			},
			wantError: `campaign "baseline": agent must not be set on a baseline campaign`,
		},
		{
			name: "baseline forbids model",
			mutate: func(config *Config) {
				campaign := config.Campaigns["baseline"]
				campaign.Model = "sonnet-5"
				config.Campaigns["baseline"] = campaign
			},
			wantError: `campaign "baseline": model must not be set on a baseline campaign`,
		},
		{
			name: "baseline forbids effort",
			mutate: func(config *Config) {
				campaign := config.Campaigns["baseline"]
				campaign.Effort = "high"
				config.Campaigns["baseline"] = campaign
			},
			wantError: `campaign "baseline": effort must not be set on a baseline campaign`,
		},
		{
			name: "baseline forbids adapter",
			mutate: func(config *Config) {
				campaign := config.Campaigns["baseline"]
				campaign.Adapter = "remote"
				config.Campaigns["baseline"] = campaign
			},
			wantError: `campaign "baseline": adapter must not be set on a baseline campaign`,
		},
		{
			name:   "valid campaign identity",
			mutate: func(*Config) {},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validCandidateIdentityConfig()
			test.mutate(&config)

			err := config.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				return
			}
			if err == nil {
				t.Fatal("Validate() error = nil, want campaign identity error")
			}
			if err.Error() != test.wantError {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), test.wantError)
			}
		})
	}
}

func TestConfigValidateRejectsStbenchHealthURLHostPortMismatch(t *testing.T) {
	tests := []struct {
		name      string
		healthURL string
		wantError string
	}{
		{
			name:      "port mismatch",
			healthURL: "http://localhost:9090/health",
			wantError: `stbench.lifecycle.health_url host/port must match base_url: got "localhost:9090", want "localhost:8080"`,
		},
		{
			name:      "host mismatch",
			healthURL: "http://candidate.test:8080/health",
			wantError: `stbench.lifecycle.health_url host/port must match base_url: got "candidate.test:8080", want "localhost:8080"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validCandidateIdentityConfig()
			config.Stbench.Lifecycle = StbenchLifecycleConfig{HealthURL: test.healthURL}

			err := config.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want host/port mismatch error")
			}
			if err.Error() != test.wantError {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), test.wantError)
			}
		})
	}
}

func TestConfigValidateAcceptsStbenchHealthURLWithMatchingHostPort(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		healthURL string
	}{
		{
			name:      "default HTTP port",
			baseURL:   "http://localhost",
			healthURL: "http://localhost/health",
		},
		{
			name:      "default HTTPS port",
			baseURL:   "https://localhost",
			healthURL: "https://localhost/health",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validCandidateIdentityConfig()
			config.BaseURL = test.baseURL
			config.Stbench.Lifecycle = StbenchLifecycleConfig{HealthURL: test.healthURL}

			if err := config.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil for matching default port", err)
			}
		})
	}
}
