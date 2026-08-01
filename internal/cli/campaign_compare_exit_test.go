package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stcompare/agentreport"
	"stcompare/internal/cli"
)

func TestCampaignCompareReturnsNotConvergedExitCodeAndWritesArtifacts(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method:         "GET",
			URL:            "http://baseline.invalid/widgets",
			ResponseStatus: http.StatusOK,
		},
	})
	writeBaselineJUnit(t, filepath.Join("reports", "baseline", "junit.xml"))

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	root := cli.NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL, "--format", "agent"})
	err := root.Execute()

	var exitErr *cli.ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("campaign compare error = %v, want ExitCodeError", err)
	}
	if exitErr.Code != agentreport.ExitCodeNotConverged {
		t.Fatalf("campaign compare exit code = %d, want %d", exitErr.Code, agentreport.ExitCodeNotConverged)
	}
	var view agentreport.View
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("campaign compare agent stdout = %q: %v", stdout.String(), err)
	}
	if view.Converged {
		t.Fatal("campaign compare agent view converged = true, want false")
	}
	if !strings.Contains(stderr.String(), "replayed 1 baseline interactions\n") {
		t.Fatalf("campaign compare agent stderr = %q", stderr.String())
	}
	for _, name := range []string{
		"replay.har.json",
		"comparison.json",
		"comparison.md",
		"comparison.html",
	} {
		if _, statErr := os.Stat(filepath.Join("reports", "gpt5.6", name)); statErr != nil {
			t.Fatalf("campaign compare artifact %q: %v", name, statErr)
		}
	}
}

func TestCampaignCompareAgentFormatWritesJSONToStdoutOnConvergence(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method:         "GET",
			URL:            "http://baseline.invalid/widgets",
			ResponseStatus: http.StatusOK,
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL, "--format", "agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("campaign compare error = %v", err)
	}

	var view agentreport.View
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("campaign compare agent stdout = %q: %v", stdout.String(), err)
	}
	if !view.Converged {
		t.Fatal("campaign compare agent view converged = false, want true")
	}
	if !strings.HasPrefix(stderr.String(), "replayed 1 baseline interactions\n") {
		t.Fatalf("campaign compare agent stderr = %q", stderr.String())
	}
	for _, name := range []string{
		"replay.har.json",
		"comparison.json",
		"comparison.md",
		"comparison.html",
	} {
		if _, statErr := os.Stat(filepath.Join("reports", "gpt5.6", name)); statErr != nil {
			t.Fatalf("campaign compare artifact %q: %v", name, statErr)
		}
	}
}

func TestCampaignCompareReturnsToolErrorExitCode(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--format", "agent"})
	err := root.Execute()

	var exitErr *cli.ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("campaign compare error = %v, want ExitCodeError", err)
	}
	if exitErr.Code != agentreport.ExitCodeToolError {
		t.Fatalf("campaign compare exit code = %d, want %d", exitErr.Code, agentreport.ExitCodeToolError)
	}
}
