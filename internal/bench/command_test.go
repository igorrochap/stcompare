package bench

import (
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
		Candidate:       "candidate",
		Agent:           "local-agent",
		Model:           "model-name",
		Hardware:        "hardware-name",
		Adapter:         "python adapter.py",
		CandidateDir:    "candidate-src",
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

	if settings.candidate != "candidate" || settings.adapter != "python adapter.py" || settings.candidateDir != "candidate-src" {
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

func TestApplyRunSettingsAcceptsPositionalCandidate(t *testing.T) {
	settings := defaultRunSettings(nil)
	applyRunSettings(&settings, runCommandOptions{candidate: "candidate"}, &cobra.Command{})
	if settings.candidate != "candidate" {
		t.Fatalf("candidate = %q, want positional candidate", settings.candidate)
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
