package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
  candidate:
    kind: candidate
stbench:
  candidate: candidate
  adapter: python adapter.py
  adapter_timeout: 3m
  lifecycle:
    stop: ./stop.sh
    build: ./build.sh
    start: ./start.sh
    command_timeout: 2m
    health_url: http://localhost:8080/health
    health_timeout: 5s
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
	if loaded.Stbench.Candidate != "candidate" || loaded.Stbench.Adapter != "python adapter.py" ||
		loaded.Stbench.AdapterTimeout != "3m" || loaded.Stbench.Lifecycle.CommandTimeout != "2m" {
		t.Fatalf("Stbench = %#v, want candidate and adapter settings", loaded.Stbench)
	}
	if loaded.Stbench.Lifecycle.HealthTimeout != "5s" {
		t.Fatalf("health timeout = %q, want %q", loaded.Stbench.Lifecycle.HealthTimeout, "5s")
	}
}

func TestConfigValidateRejectsTrimmedDuplicatePreconditionHeuristicNames(t *testing.T) {
	config := Default()
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
			config := Default()
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
				"gpt5.6":  {Kind: "candidate"},
				"sonnet5": {Kind: "candidate"},
			},
			wantError: "exactly one baseline campaign is required: found none",
		},
		{
			name: "one baseline campaign",
			campaigns: map[string]Campaign{
				"reference": {Kind: "baseline"},
				"gpt5.6":    {Kind: "candidate"},
			},
		},
		{
			name: "multiple baseline campaigns",
			campaigns: map[string]Campaign{
				"reference": {Kind: "baseline"},
				"control":   {Kind: "baseline"},
				"gpt5.6":    {Kind: "candidate"},
			},
			wantError: "exactly one baseline campaign is required: found 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
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
