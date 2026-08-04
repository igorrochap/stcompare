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

func TestCampaignCompareUsesCandidateOwnedSpecForStatusCodeConformance(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	defaultConfig["candidate_spec"] = "/openapi.json"
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "POST",
			URL:    "http://baseline.invalid/widgets?dryRun=true",
			Headers: []harHeaderFixture{
				{Name: "X-Schemathesis-TestCaseId", Value: "case-42"},
			},
			ResponseStatus: http.StatusTeapot,
		},
	})
	writeBaselineVCR(t, filepath.Join("reports", "baseline", "campaign.vcr.yaml"))

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/openapi.json" {
			response.Header().Set("Content-Type", "application/yaml")
			_, _ = response.Write([]byte(`
openapi: 3.0.3
info:
  title: Candidate API
  version: "1.0"
paths:
  /widgets:
    post:
      responses:
        "418":
          description: documented teapot
`))
			return
		}
		response.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)

	root := cli.NewRootCommand()
	root.SetArgs([]string{
		"campaign", "compare", "gpt5.6",
		"--base-url", server.URL,
		"--format", "agent",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("campaign compare error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.json"))
	if err != nil {
		t.Fatalf("read comparison report: %v", err)
	}
	var report struct {
		Problems []struct {
			Outcome       string `json:"outcome"`
			OutcomeReason string `json:"outcome_reason"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("decode comparison report: %v", err)
	}
	if len(report.Problems) != 1 || report.Problems[0].Outcome != "fixed" ||
		report.Problems[0].OutcomeReason != "status_code_documented" {
		t.Fatalf("candidate status-code outcome = %#v, want documented fixed", report.Problems)
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
