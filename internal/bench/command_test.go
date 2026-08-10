package bench

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"stcompare/benchrecord"
	"stcompare/internal/config"
)

func TestDefaultRunSettingsLoadsStbenchConfiguration(t *testing.T) {
	settings := defaultRunSettings(&config.StbenchConfig{
		Campaign:        "campaign",
		Agent:           "local-agent",
		Model:           "model-name",
		Hardware:        "hardware-name",
		Adapter:         "python adapter.py",
		ReuseProcess:    true,
		SourceDir:       "candidate-src",
		StcompareBinary: "./stcompare",
		RecordPath:      "records/run.json",
		Prompt:          config.StbenchPromptConfig{ID: "prompt", Version: "2"},
		Lifecycle: config.StbenchLifecycleConfig{
			Stop:           "./stop.sh",
			Reset:          "./reset.sh",
			Build:          "./build.sh",
			Start:          "./start.sh",
			HealthURL:      "http://localhost:8080/health",
			HealthTimeout:  "5s",
			HealthInterval: "10ms",
			CommandTimeout: "2m",
		},
		AdapterTimeout: "3m",
		MaxIterations:  7,
		StallWindow:    3,
	})

	if settings.campaign != "campaign" || settings.adapter != "python adapter.py" || !settings.reuseProcess || settings.sourceDir != "candidate-src" {
		t.Fatalf("settings = %#v, want caller configuration", settings)
	}
	if settings.stop != "./stop.sh" || settings.reset != "./reset.sh" || settings.healthTimeout != "5s" ||
		settings.commandTimeout != "2m" || settings.adapterTimeout != "3m" {
		t.Fatalf("lifecycle settings = %#v, want caller configuration", settings)
	}
	if settings.maxIterations != 7 || settings.stallWindow != 3 || settings.promptVersion != "2" {
		t.Fatalf("run limits/prompt = %#v, want caller configuration", settings)
	}
}

func TestApplyRunSettingsAcceptsPositionalCampaign(t *testing.T) {
	settings := defaultRunSettings(nil)
	applyRunSettings(&settings, runCommandOptions{campaign: "campaign"}, &cobra.Command{})
	if settings.campaign != "campaign" {
		t.Fatalf("campaign = %q, want positional campaign", settings.campaign)
	}
}

func TestRunCommandUsesOneCanonicalFlagPerSetting(t *testing.T) {
	root := NewRootCommand()
	run, _, err := root.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run command: %v", err)
	}

	canonical := []string{
		"campaign", "agent", "model", "hardware", "adapter", "adapter-timeout", "reuse-process",
		"source-dir", "stcompare-binary", "record-path", "emit-scorecard", "base-url",
		"stop-command", "reset-command", "build-command", "start-command",
		"command-timeout", "health-url", "health-timeout", "health-interval",
		"max-iterations", "stall-window", "prompt-id", "prompt-version",
	}
	for _, name := range canonical {
		if run.Flags().Lookup(name) == nil {
			t.Errorf("canonical flag --%s is not registered", name)
		}
	}

	aliases := []string{
		"candidate", "adapter-command", "candidate-dir", "stcompare", "record",
		"stop", "reset", "build", "start",
	}
	for _, name := range aliases {
		if run.Flags().Lookup(name) != nil {
			t.Errorf("legacy alias --%s is still registered", name)
		}
	}
}

func TestApplyRunSettingsEnablesScorecardOnlyWhenFlagIsPresent(t *testing.T) {
	command := &cobra.Command{}
	command.Flags().Bool("emit-scorecard", false, "")

	settings := defaultRunSettings(nil)
	applyRunSettings(&settings, runCommandOptions{}, command)
	if settings.emitScorecard {
		t.Fatal("emitScorecard = true without flag, want false")
	}

	if err := command.Flags().Set("emit-scorecard", "true"); err != nil {
		t.Fatalf("set --emit-scorecard: %v", err)
	}
	applyRunSettings(&settings, runCommandOptions{emitScorecard: true}, command)
	if !settings.emitScorecard {
		t.Fatal("emitScorecard = false with flag, want true")
	}
}

func TestEmitScorecardIfRequestedBuildsFromCandidateReport(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "candidate", "comparison.json")
	if err := os.MkdirAll(filepath.Dir(comparisonPath), 0o755); err != nil {
		t.Fatalf("create candidate report directory: %v", err)
	}
	if err := os.WriteFile(comparisonPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write comparison output: %v", err)
	}

	builder := &fakeScorecardBuilder{writeOutput: true}
	var warnings bytes.Buffer
	emitScorecardIfRequested(scorecardRequested, builder, scorecardBuildInput{
		ComparisonPath: comparisonPath,
		RecordPath:     filepath.Join(directory, "benchmark-record.json"),
		OutputPath:     filepath.Join(directory, "candidate", "scorecard.html"),
	}, &warnings)

	if len(builder.inputs) != 1 {
		t.Fatalf("scorecard build calls = %d, want 1", len(builder.inputs))
	}
	if got := builder.inputs[0]; got.ComparisonPath != comparisonPath ||
		got.RecordPath != filepath.Join(directory, "benchmark-record.json") ||
		got.OutputPath != filepath.Join(directory, "candidate", "scorecard.html") {
		t.Fatalf("scorecard build input = %#v, want derived comparison, record, and output paths", got)
	}
	if warnings.Len() != 0 {
		t.Fatalf("warnings = %q, want none", warnings.String())
	}
	if _, err := os.Stat(filepath.Join(directory, "candidate", "scorecard.html")); err != nil {
		t.Fatalf("scorecard output: %v", err)
	}
}

func TestEmitScorecardIfRequestedSkipsMissingComparisonWithoutFailing(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "candidate", "comparison.json")
	builder := &fakeScorecardBuilder{}
	var warnings bytes.Buffer

	emitScorecardIfRequested(scorecardRequested, builder, scorecardBuildInput{
		ComparisonPath: comparisonPath,
		RecordPath:     filepath.Join(directory, "benchmark-record.json"),
		OutputPath:     filepath.Join(directory, "candidate", "scorecard.html"),
	}, &warnings)

	if len(builder.inputs) != 0 {
		t.Fatalf("scorecard build calls = %d, want 0", len(builder.inputs))
	}
	if got := warnings.String(); !strings.Contains(got, "warning: scorecard not generated") ||
		!strings.Contains(got, comparisonPath) || !strings.Contains(got, "does not exist") {
		t.Fatalf("warning = %q, want missing comparison guidance", got)
	}
}

func TestEmitScorecardIfRequestedLeavesAbsentFlagInert(t *testing.T) {
	builder := &fakeScorecardBuilder{err: errors.New("must not run")}
	var warnings bytes.Buffer

	emitScorecardIfRequested(scorecardNotRequested, builder, scorecardBuildInput{}, &warnings)

	if len(builder.inputs) != 0 {
		t.Fatalf("scorecard build calls = %d, want 0", len(builder.inputs))
	}
	if warnings.Len() != 0 {
		t.Fatalf("warnings = %q, want none", warnings.String())
	}
}

func TestEmitScorecardIfRequestedWarnsWhenBuilderFails(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "comparison.json")
	if err := os.WriteFile(comparisonPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write comparison output: %v", err)
	}
	builder := &fakeScorecardBuilder{err: errors.New("subprocess failed")}
	var warnings bytes.Buffer

	emitScorecardIfRequested(scorecardRequested, builder, scorecardBuildInput{
		ComparisonPath: comparisonPath,
	}, &warnings)

	if got := warnings.String(); !strings.Contains(got, "warning: scorecard not generated") ||
		!strings.Contains(got, "subprocess failed") {
		t.Fatalf("warning = %q, want subprocess failure", got)
	}
}

func TestApplyRunSettingsFlagsOverrideConfiguration(t *testing.T) {
	command := &cobra.Command{}
	command.Flags().String("campaign", "", "")
	command.Flags().String("source-dir", "", "")
	command.Flags().String("adapter", "", "")
	command.Flags().String("record-path", "", "")
	command.Flags().String("stop-command", "", "")
	for name, value := range map[string]string{
		"campaign":     "flag-campaign",
		"source-dir":   "flag-source",
		"adapter":      "flag-adapter",
		"record-path":  "flag-record.json",
		"stop-command": "flag-stop",
	} {
		if err := command.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}

	settings := runSettings{
		campaign:   "config-campaign",
		sourceDir:  "config-source",
		adapter:    "config-adapter",
		recordPath: "config-record.json",
		stop:       "config-stop",
	}
	applyRunSettings(&settings, runCommandOptions{
		campaign:   "flag-campaign",
		sourceDir:  "flag-source",
		adapter:    "flag-adapter",
		recordPath: "flag-record.json",
		stop:       "flag-stop",
	}, command)

	if settings.campaign != "flag-campaign" || settings.sourceDir != "flag-source" ||
		settings.adapter != "flag-adapter" || settings.recordPath != "flag-record.json" ||
		settings.stop != "flag-stop" {
		t.Fatalf("settings = %#v, want explicit flag values to override config", settings)
	}
}

func TestApplyRunOverridesBaseURLFlag(t *testing.T) {
	command := &cobra.Command{}
	command.Flags().String("base-url", "", "")
	if err := command.Flags().Set("base-url", "http://flag.example.test:9090"); err != nil {
		t.Fatalf("set --base-url: %v", err)
	}

	effective := config.Default()
	effective.BaseURL = "http://config.example.test:8080"
	applyRunOverrides(command, &effective, &runCommandOptions{baseURL: "http://flag.example.test:9090"})

	if effective.BaseURL != "http://flag.example.test:9090" {
		t.Fatalf("base URL = %q, want explicit flag value", effective.BaseURL)
	}
}

func TestApplyRunSettingsAcceptsTimeoutFlags(t *testing.T) {
	command := &cobra.Command{}
	command.Flags().String("adapter-timeout", "", "")
	command.Flags().String("command-timeout", "", "")
	if err := command.Flags().Set("adapter-timeout", "2s"); err != nil {
		t.Fatalf("set adapter timeout flag: %v", err)
	}
	if err := command.Flags().Set("command-timeout", "3s"); err != nil {
		t.Fatalf("set command timeout flag: %v", err)
	}

	settings := defaultRunSettings(nil)
	applyRunSettings(&settings, runCommandOptions{
		adapterTimeout: "2s",
		commandTimeout: "3s",
	}, command)
	if settings.adapterTimeout != "2s" || settings.commandTimeout != "3s" {
		t.Fatalf("timeouts = %q and %q, want flag values", settings.adapterTimeout, settings.commandTimeout)
	}
}

func TestParseDurationAcceptsGoDurationAndRejectsInvalidValues(t *testing.T) {
	got, err := parseDuration("health timeout", "250ms")
	if err != nil {
		t.Fatalf("parseDuration() error = %v", err)
	}
	if got != 250*time.Millisecond {
		t.Fatalf("duration = %s, want 250ms", got)
	}

	if _, err := parseDuration("health timeout", "not-a-duration"); err == nil || !strings.Contains(err.Error(), "health timeout") {
		t.Fatalf("parseDuration() error = %v, want named duration error", err)
	}
	if _, err := parseDuration("health timeout", "-1s"); err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("parseDuration() error = %v, want negative-duration error", err)
	}
}

func TestExitCodeForBenchmarkTerminalState(t *testing.T) {
	for _, test := range []struct {
		state benchrecord.TerminalState
		want  int
	}{
		{state: benchrecord.TerminalStateConverged, want: 0},
		{state: benchrecord.TerminalStateStalled, want: 2},
		{state: benchrecord.TerminalStateMaxIterations, want: 2},
		{state: benchrecord.TerminalStateToolError, want: 1},
		{state: benchrecord.TerminalStateAdapterError, want: 1},
		{state: benchrecord.TerminalStateLifecycleError, want: 1},
	} {
		if got := exitCodeForState(test.state); got != test.want {
			t.Errorf("exitCodeForState(%q) = %d, want %d", test.state, got, test.want)
		}
	}
}

func TestWriteRecordCreatesParentDirectoryAndJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "record.json")
	record := benchrecord.Record{
		SchemaVersion: benchrecord.SchemaVersion,
		TerminalState: benchrecord.TerminalStateConverged,
		Tokens:        nil,
	}
	if err := writeRecord(path, record); err != nil {
		t.Fatalf("writeRecord() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if !strings.Contains(string(contents), `"terminal_state": "converged"`) || !strings.Contains(string(contents), `"tokens": null`) {
		t.Fatalf("record = %s, want terminal state and null tokens", contents)
	}
}

type fakeScorecardBuilder struct {
	inputs      []scorecardBuildInput
	err         error
	writeOutput bool
}

const (
	scorecardNotRequested = false
	scorecardRequested    = true
)

func (builder *fakeScorecardBuilder) Build(input scorecardBuildInput) error {
	builder.inputs = append(builder.inputs, input)
	if builder.writeOutput {
		return os.WriteFile(input.OutputPath, []byte("scorecard"), 0o644)
	}
	return builder.err
}
