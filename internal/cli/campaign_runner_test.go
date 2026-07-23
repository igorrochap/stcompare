package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestExecCampaignRunnerFallsBackToUvxSchemathesisWhenStIsUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "uvx.log")
	uvxPath := filepath.Join(binDir, "uvx")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$2\" = \"--version\" ]; then printf 'Schemathesis 4.0.0\\n'; fi\n"
	if err := os.WriteFile(uvxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake uvx: %v", err)
	}
	t.Setenv("PATH", binDir)

	runner := execCampaignRunner{}
	version, versionErr := runner.SchemathesisVersion()
	runErr := runner.Run([]string{"st", "run", "openapi.json"})

	logContents, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read uvx log: %v", readErr)
	}
	got := struct {
		Version    string
		VersionErr string
		RunErr     string
		Log        []string
	}{
		Version: version,
		Log:     strings.Split(strings.TrimSpace(string(logContents)), "\n"),
	}
	if versionErr != nil {
		got.VersionErr = versionErr.Error()
	}
	if runErr != nil {
		got.RunErr = runErr.Error()
	}
	want := struct {
		Version    string
		VersionErr string
		RunErr     string
		Log        []string
	}{
		Version: "Schemathesis 4.0.0",
		Log: []string{
			"schemathesis --version",
			"schemathesis run openapi.json",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runner outcome = %#v, want %#v", got, want)
	}
}

func TestExecCampaignRunnerUsesConfiguredSchemathesisCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tool.log")
	toolPath := filepath.Join(binDir, "schemathesis-tool")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$1\" = \"--version\" ]; then printf 'Schemathesis custom\\n'; fi\n"
	if err := os.WriteFile(toolPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("STCOMPARE_SCHEMATHESIS_COMMAND", "schemathesis-tool")

	runner := execCampaignRunner{}
	version, versionErr := runner.SchemathesisVersion()
	runErr := runner.Run([]string{"st", "run", "openapi.json"})

	logContents, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read tool log: %v", readErr)
	}
	got := struct {
		Version    string
		VersionErr string
		RunErr     string
		Log        []string
	}{
		Version: version,
		Log:     strings.Split(strings.TrimSpace(string(logContents)), "\n"),
	}
	if versionErr != nil {
		got.VersionErr = versionErr.Error()
	}
	if runErr != nil {
		got.RunErr = runErr.Error()
	}
	want := struct {
		Version    string
		VersionErr string
		RunErr     string
		Log        []string
	}{
		Version: "Schemathesis custom",
		Log: []string{
			"--version",
			"run openapi.json",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runner outcome = %#v, want %#v", got, want)
	}
}

func TestExecCampaignRunnerTreatsSchemathesisFailuresAsCompletedRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}

	binDir := t.TempDir()
	stPath := filepath.Join(binDir, "st")
	script := "#!/bin/sh\n" +
		"exit 1\n"
	if err := os.WriteFile(stPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake st: %v", err)
	}
	t.Setenv("PATH", binDir)

	runner := execCampaignRunner{}
	err := runner.Run([]string{"st", "run", "openapi.json"})

	if err != nil {
		t.Fatalf("run returned error for Schemathesis finding exit code: %v", err)
	}
}

func TestExecCampaignRunnerReturnsErrorForSchemathesisAbortExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}

	binDir := t.TempDir()
	stPath := filepath.Join(binDir, "st")
	script := "#!/bin/sh\n" +
		"exit 2\n"
	if err := os.WriteFile(stPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake st: %v", err)
	}
	t.Setenv("PATH", binDir)

	runner := execCampaignRunner{}
	err := runner.Run([]string{"st", "run", "openapi.json"})

	if err == nil {
		t.Fatal("run returned nil for Schemathesis abort exit code")
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
