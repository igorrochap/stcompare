package cli_test

import (
	"bytes"
	"testing"

	"stcompare/internal/cli"
)

func TestCampaignCommandPrintsDefaultRunCommand(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"campaign", "command", "baseline"})
	err := root.Execute()

	got := configCommandOutcome{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	}
	want := configCommandOutcome{
		Output: "st run openapi.json --url http://localhost:8080 --workers 1 --seed 12345 --generation-deterministic --generation-database none --report junit,vcr,har,ndjson --report-junit-path reports/baseline/junit.xml --report-vcr-path reports/baseline/campaign.vcr.yaml --report-har-path reports/baseline/campaign.har.json --report-ndjson-path reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false\n",
	}

	if got != want {
		t.Fatalf("campaign command outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCommandAppendsConfiguredSchemathesisExtraArgs(t *testing.T) {
	input := loadDefaultConfig(t)
	configSection(t, input, "schemathesis")["extra_args"] = []any{"--checks", "all"}
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", input)

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"campaign", "command", "baseline"})
	err := root.Execute()

	got := configCommandOutcome{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	}
	want := configCommandOutcome{
		Output: "st run openapi.json --url http://localhost:8080 --workers 1 --seed 12345 --generation-deterministic --generation-database none --report junit,vcr,har,ndjson --report-junit-path reports/baseline/junit.xml --report-vcr-path reports/baseline/campaign.vcr.yaml --report-har-path reports/baseline/campaign.har.json --report-ndjson-path reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false --checks all\n",
	}

	if got != want {
		t.Fatalf("campaign command outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCommandHonorsCommonOverrides(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{
		"campaign", "command", "baseline",
		"--schema", "api/openapi.yaml",
		"--base-url", "http://localhost:9090",
		"--reports-dir", "comparison-reports",
		"--seed", "4242",
		"--workers", "8",
	})
	err := root.Execute()

	got := configCommandOutcome{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	}
	want := configCommandOutcome{
		Output: "st run api/openapi.yaml --url http://localhost:9090 --workers 8 --seed 4242 --generation-deterministic --generation-database none --report junit,vcr,har,ndjson --report-junit-path comparison-reports/baseline/junit.xml --report-vcr-path comparison-reports/baseline/campaign.vcr.yaml --report-har-path comparison-reports/baseline/campaign.har.json --report-ndjson-path comparison-reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false\n",
	}

	if got != want {
		t.Fatalf("campaign command outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCommandRejectsExtraArgsThatOverrideToolOwnedReports(t *testing.T) {
	tests := []struct {
		name      string
		extraArgs []any
		wantError string
	}{
		{
			name:      "report format",
			extraArgs: []any{"--report", "junit"},
			wantError: `schemathesis.extra_args cannot override tool-owned report option "--report"`,
		},
		{
			name:      "junit path",
			extraArgs: []any{"--report-junit-path=custom.xml"},
			wantError: `schemathesis.extra_args cannot override tool-owned report option "--report-junit-path"`,
		},
		{
			name:      "vcr path",
			extraArgs: []any{"--report-vcr-path", "custom.vcr.yaml"},
			wantError: `schemathesis.extra_args cannot override tool-owned report option "--report-vcr-path"`,
		},
		{
			name:      "har path",
			extraArgs: []any{"--report-har-path=custom.har"},
			wantError: `schemathesis.extra_args cannot override tool-owned report option "--report-har-path"`,
		},
		{
			name:      "ndjson path",
			extraArgs: []any{"--report-ndjson-path", "custom.ndjson"},
			wantError: `schemathesis.extra_args cannot override tool-owned report option "--report-ndjson-path"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := loadDefaultConfig(t)
			configSection(t, input, "schemathesis")["extra_args"] = tt.extraArgs
			t.Chdir(t.TempDir())
			writeConfig(t, "stcompare.yaml", input)

			var output bytes.Buffer
			root := cli.NewRootCommand()
			root.SetOut(&output)
			root.SetArgs([]string{"campaign", "command", "baseline"})
			err := root.Execute()

			got := configCommandOutcome{Output: output.String()}
			if err != nil {
				got.Error = err.Error()
			}
			want := configCommandOutcome{Error: tt.wantError}

			if got != want {
				t.Fatalf("campaign command outcome = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCampaignCommandRejectsUnsafeCampaignNames(t *testing.T) {
	tests := []struct {
		name         string
		campaignName string
		wantError    string
	}{
		{
			name:         "unknown campaign",
			campaignName: "missing",
			wantError:    `campaign "missing" is not configured`,
		},
		{
			name:         "path traversal",
			campaignName: "../baseline",
			wantError:    `campaign name "../baseline" is invalid: use letters, numbers, dots, underscores, or hyphens`,
		},
		{
			name:         "current directory",
			campaignName: ".",
			wantError:    `campaign name "." is invalid: use letters, numbers, dots, underscores, or hyphens`,
		},
		{
			name:         "parent directory",
			campaignName: "..",
			wantError:    `campaign name ".." is invalid: use letters, numbers, dots, underscores, or hyphens`,
		},
		{
			name:         "whitespace",
			campaignName: "bad name",
			wantError:    `campaign name "bad name" is invalid: use letters, numbers, dots, underscores, or hyphens`,
		},
		{
			name:         "backslash",
			campaignName: `bad\name`,
			wantError:    `campaign name "bad\\name" is invalid: use letters, numbers, dots, underscores, or hyphens`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultConfig := loadDefaultConfig(t)
			t.Chdir(t.TempDir())
			writeConfig(t, "stcompare.yaml", defaultConfig)

			var output bytes.Buffer
			root := cli.NewRootCommand()
			root.SetOut(&output)
			root.SetArgs([]string{"campaign", "command", tt.campaignName})
			err := root.Execute()

			got := configCommandOutcome{Output: output.String()}
			if err != nil {
				got.Error = err.Error()
			}
			want := configCommandOutcome{Error: tt.wantError}

			if got != want {
				t.Fatalf("campaign command outcome = %#v, want %#v", got, want)
			}
		})
	}
}
