package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"stcompare/internal/cli"
)

type recordingCampaignRunner struct {
	argv                 []string
	schemathesisVersion  string
	schemathesisVersionE error
	runErr               error
	runFunc              func([]string) error
}

func (runner *recordingCampaignRunner) SchemathesisVersion() (string, error) {
	return runner.schemathesisVersion, runner.schemathesisVersionE
}

func (runner *recordingCampaignRunner) Run(argv []string) error {
	runner.argv = append([]string(nil), argv...)
	if runner.runFunc != nil {
		return runner.runFunc(argv)
	}

	return runner.runErr
}

func executeCampaignRunWithRunner(runner *recordingCampaignRunner, args ...string) error {
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{
		CampaignRunner: runner,
		Now: func() time.Time {
			return time.Date(2026, 7, 23, 12, 34, 56, 0, time.UTC)
		},
		ToolVersion: "test-version",
	})
	root.SetArgs(args)

	return root.Execute()
}

func TestCampaignRunBaselineWritesReportsAndMetadata(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	runner := &recordingCampaignRunner{schemathesisVersion: "Schemathesis 4.0.0"}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline")

	metadataPath := filepath.Join("reports", "baseline", "metadata.yaml")
	metadataContents, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read campaign metadata: %v", readErr)
	}
	gotMetadata := decodeConfig(t, metadataContents)
	got := struct {
		Error            string
		Runner           []string
		Campaign         map[string]any
		ConfigPath       any
		EffectiveCommand any
		ToolVersion      any
		STVersion        any
		Timestamp        any
	}{
		Runner:           runner.argv,
		Campaign:         configSection(t, gotMetadata, "campaign"),
		ConfigPath:       gotMetadata["config_path"],
		EffectiveCommand: gotMetadata["effective_command"],
		ToolVersion:      gotMetadata["tool_version"],
		STVersion:        gotMetadata["schemathesis_version"],
		Timestamp:        gotMetadata["timestamp"],
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error            string
		Runner           []string
		Campaign         map[string]any
		ConfigPath       any
		EffectiveCommand any
		ToolVersion      any
		STVersion        any
		Timestamp        any
	}{
		Runner: []string{
			"st", "run", "openapi.json",
			"--url", "http://localhost:8080",
			"--workers", "1",
			"--seed", "12345",
			"--generation-deterministic",
			"--report", "junit,vcr,har,ndjson",
			"--report-junit-path", filepath.Join("reports", "baseline", "junit.xml"),
			"--report-vcr-path", filepath.Join("reports", "baseline", "campaign.vcr.yaml"),
			"--report-har-path", filepath.Join("reports", "baseline", "campaign.har.json"),
			"--report-ndjson-path", filepath.Join("reports", "baseline", "campaign.ndjson"),
			"--output-sanitize", "false",
			"--output-truncate", "false",
		},
		Campaign: map[string]any{
			"name": "baseline",
			"kind": "baseline",
		},
		ConfigPath: "stcompare.yaml",
		EffectiveCommand: []any{
			"st", "run", "openapi.json",
			"--url", "http://localhost:8080",
			"--workers", "1",
			"--seed", "12345",
			"--generation-deterministic",
			"--report", "junit,vcr,har,ndjson",
			"--report-junit-path", filepath.Join("reports", "baseline", "junit.xml"),
			"--report-vcr-path", filepath.Join("reports", "baseline", "campaign.vcr.yaml"),
			"--report-har-path", filepath.Join("reports", "baseline", "campaign.har.json"),
			"--report-ndjson-path", filepath.Join("reports", "baseline", "campaign.ndjson"),
			"--output-sanitize", "false",
			"--output-truncate", "false",
		},
		ToolVersion: "test-version",
		STVersion:   "Schemathesis 4.0.0",
		Timestamp:   "2026-07-23T12:34:56Z",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunBaselineSnapshotsSchema(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	const schema = "openapi: 3.0.3\ninfo:\n  title: frozen\n  version: \"1.0\"\npaths: {}\n"
	if err := os.WriteFile("openapi.json", []byte(schema), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	runner := &recordingCampaignRunner{schemathesisVersion: "Schemathesis 4.0.0"}
	if err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline"); err != nil {
		t.Fatalf("campaign run error = %v", err)
	}
	if err := os.WriteFile("openapi.json", []byte("mutated"), 0o644); err != nil {
		t.Fatalf("mutate schema: %v", err)
	}

	snapshot, err := os.ReadFile(filepath.Join("reports", "baseline", "schema.snapshot"))
	if err != nil {
		t.Fatalf("read schema snapshot: %v", err)
	}
	if string(snapshot) != schema {
		t.Fatalf("schema snapshot = %q, want %q", snapshot, schema)
	}
	metadataContents, err := os.ReadFile(filepath.Join("reports", "baseline", "metadata.yaml"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	metadata := decodeConfig(t, metadataContents)
	if metadata["schema_snapshot"] != filepath.Join("reports", "baseline", "schema.snapshot") {
		t.Fatalf("schema snapshot metadata = %#v", metadata["schema_snapshot"])
	}
}

func TestCampaignRunRefusesToOverwriteExistingBaselineReports(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	reportDir := filepath.Join("reports", "baseline")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("create existing report dir: %v", err)
	}
	metadataPath := filepath.Join(reportDir, "metadata.yaml")
	const sentinel = "sentinel: keep\n"
	if err := os.WriteFile(metadataPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel metadata: %v", err)
	}

	runner := &recordingCampaignRunner{schemathesisVersion: "Schemathesis 4.0.0"}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline")

	metadataContents, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read sentinel metadata: %v", readErr)
	}
	got := struct {
		Error           string
		RunnerWasCalled bool
		Metadata        string
	}{
		RunnerWasCalled: runner.argv != nil,
		Metadata:        string(metadataContents),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error           string
		RunnerWasCalled bool
		Metadata        string
	}{
		Error:    "campaign report directory reports/baseline already exists; use --force to overwrite",
		Metadata: sentinel,
	}

	if got != want {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunForceOverwritesExistingBaselineMetadata(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	reportDir := filepath.Join("reports", "baseline")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("create existing report dir: %v", err)
	}
	metadataPath := filepath.Join(reportDir, "metadata.yaml")
	if err := os.WriteFile(metadataPath, []byte("sentinel: replace\n"), 0o644); err != nil {
		t.Fatalf("write sentinel metadata: %v", err)
	}

	runner := &recordingCampaignRunner{schemathesisVersion: "Schemathesis 4.0.0"}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline", "--force")

	metadataContents, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read campaign metadata: %v", readErr)
	}
	got := struct {
		Error           string
		RunnerWasCalled bool
		Campaign        map[string]any
	}{
		RunnerWasCalled: runner.argv != nil,
	}
	if err != nil {
		got.Error = err.Error()
	} else {
		gotMetadata := decodeConfig(t, metadataContents)
		got.Campaign = configSection(t, gotMetadata, "campaign")
	}
	want := struct {
		Error           string
		RunnerWasCalled bool
		Campaign        map[string]any
	}{
		RunnerWasCalled: true,
		Campaign: map[string]any{
			"name": "baseline",
			"kind": "baseline",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunCandidateRecordsSettingsAndOverrides(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	runner := &recordingCampaignRunner{schemathesisVersion: "Schemathesis 4.0.0"}
	err := executeCampaignRunWithRunner(runner,
		"campaign", "run", "gpt5.6",
		"--schema", "api/openapi.yaml",
		"--base-url", "http://localhost:9090",
		"--reports-dir", "comparison-reports",
		"--seed", "4242",
		"--workers", "8",
	)

	metadataPath := filepath.Join("comparison-reports", "gpt5.6", "metadata.yaml")
	metadataContents, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read campaign metadata: %v", readErr)
	}
	gotMetadata := decodeConfig(t, metadataContents)
	got := struct {
		Error     string
		Campaign  map[string]any
		Settings  any
		Overrides any
	}{
		Campaign:  configSection(t, gotMetadata, "campaign"),
		Settings:  gotMetadata["settings"],
		Overrides: gotMetadata["overrides"],
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error     string
		Campaign  map[string]any
		Settings  any
		Overrides any
	}{
		Campaign: map[string]any{
			"name": "gpt5.6",
			"kind": "candidate",
		},
		Settings: map[string]any{
			"schema":                   "api/openapi.yaml",
			"base_url":                 "http://localhost:9090",
			"reports_dir":              "comparison-reports",
			"seed":                     4242,
			"workers":                  8,
			"generation_deterministic": true,
			"generation_database":      "none",
			"reports":                  []any{"junit", "vcr", "har", "ndjson"},
			"output_sanitize":          false,
			"output_truncate":          false,
			"extra_args":               []any{},
		},
		Overrides: map[string]any{
			"schema":      "api/openapi.yaml",
			"base_url":    "http://localhost:9090",
			"reports_dir": "comparison-reports",
			"seed":        4242,
			"workers":     8,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunRemovesNewReportDirectoryWhenRunnerFails(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	runner := &recordingCampaignRunner{
		schemathesisVersion: "Schemathesis 4.0.0",
		runErr:              errors.New("st failed"),
	}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline")

	_, statErr := os.Stat(filepath.Join("reports", "baseline"))
	got := struct {
		Error         string
		ReportDirGone bool
	}{
		ReportDirGone: errors.Is(statErr, os.ErrNotExist),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error         string
		ReportDirGone bool
	}{
		Error:         "st failed",
		ReportDirGone: true,
	}

	if got != want {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunKeepFailedPreservesNewPartialArtifactsWithoutMetadata(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	runner := &recordingCampaignRunner{
		schemathesisVersion: "Schemathesis 4.0.0",
		runFunc: func([]string) error {
			partialPath := filepath.Join("reports", "baseline", "campaign.ndjson")
			if err := os.WriteFile(partialPath, []byte("partial\n"), 0o644); err != nil {
				t.Fatalf("write partial artifact: %v", err)
			}

			return errors.New("st failed")
		},
	}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline", "--keep-failed")

	partialContents, readErr := os.ReadFile(filepath.Join("reports", "baseline", "campaign.ndjson"))
	if readErr != nil {
		t.Fatalf("read partial artifact: %v", readErr)
	}
	_, metadataErr := os.Stat(filepath.Join("reports", "baseline", "metadata.yaml"))
	got := struct {
		Error        string
		Partial      string
		MetadataGone bool
	}{
		Partial:      string(partialContents),
		MetadataGone: errors.Is(metadataErr, os.ErrNotExist),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error        string
		Partial      string
		MetadataGone bool
	}{
		Error:        "st failed; preserved partial debug artifacts in reports/baseline, but this is not a completed campaign",
		Partial:      "partial\n",
		MetadataGone: true,
	}

	if got != want {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunForceKeepsExistingReportDirectoryWhenRunnerFails(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	reportDir := filepath.Join("reports", "baseline")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("create existing report dir: %v", err)
	}
	metadataPath := filepath.Join(reportDir, "metadata.yaml")
	const sentinel = "sentinel: keep\n"
	if err := os.WriteFile(metadataPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel metadata: %v", err)
	}

	runner := &recordingCampaignRunner{
		schemathesisVersion: "Schemathesis 4.0.0",
		runErr:              errors.New("st failed"),
	}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline", "--force")

	metadataContents, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read sentinel metadata: %v", readErr)
	}
	got := struct {
		Error    string
		Metadata string
	}{Metadata: string(metadataContents)}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Metadata string
	}{
		Error:    "st failed",
		Metadata: sentinel,
	}

	if got != want {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignRunForceKeepFailedReturnsPlainErrorForExistingReportDirectory(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	reportDir := filepath.Join("reports", "baseline")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("create existing report dir: %v", err)
	}
	metadataPath := filepath.Join(reportDir, "metadata.yaml")
	const sentinel = "sentinel: keep\n"
	if err := os.WriteFile(metadataPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write sentinel metadata: %v", err)
	}

	runner := &recordingCampaignRunner{
		schemathesisVersion: "Schemathesis 4.0.0",
		runErr:              errors.New("st failed"),
	}
	err := executeCampaignRunWithRunner(runner, "campaign", "run", "baseline", "--force", "--keep-failed")

	metadataContents, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read sentinel metadata: %v", readErr)
	}
	got := struct {
		Error    string
		Metadata string
	}{Metadata: string(metadataContents)}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Metadata string
	}{
		Error:    "st failed",
		Metadata: sentinel,
	}

	if got != want {
		t.Fatalf("campaign run outcome = %#v, want %#v", got, want)
	}
}

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
		Output: "st run openapi.json --url http://localhost:8080 --workers 1 --seed 12345 --generation-deterministic --report junit,vcr,har,ndjson --report-junit-path reports/baseline/junit.xml --report-vcr-path reports/baseline/campaign.vcr.yaml --report-har-path reports/baseline/campaign.har.json --report-ndjson-path reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false\n",
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
		Output: "st run openapi.json --url http://localhost:8080 --workers 1 --seed 12345 --generation-deterministic --report junit,vcr,har,ndjson --report-junit-path reports/baseline/junit.xml --report-vcr-path reports/baseline/campaign.vcr.yaml --report-har-path reports/baseline/campaign.har.json --report-ndjson-path reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false --checks all\n",
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
		Output: "st run api/openapi.yaml --url http://localhost:9090 --workers 8 --seed 4242 --generation-deterministic --report junit,vcr,har,ndjson --report-junit-path comparison-reports/baseline/junit.xml --report-vcr-path comparison-reports/baseline/campaign.vcr.yaml --report-har-path comparison-reports/baseline/campaign.har.json --report-ndjson-path comparison-reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false\n",
	}

	if got != want {
		t.Fatalf("campaign command outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCommandIncludesGenerationDatabaseWhenDeterministicGenerationIsDisabled(t *testing.T) {
	input := loadDefaultConfig(t)
	schemathesis := configSection(t, input, "schemathesis")
	schemathesis["generation_deterministic"] = false
	schemathesis["generation_database"] = "examples.db"
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
		Output: "st run openapi.json --url http://localhost:8080 --workers 1 --seed 12345 --generation-database examples.db --report junit,vcr,har,ndjson --report-junit-path reports/baseline/junit.xml --report-vcr-path reports/baseline/campaign.vcr.yaml --report-har-path reports/baseline/campaign.har.json --report-ndjson-path reports/baseline/campaign.ndjson --output-sanitize false --output-truncate false\n",
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
