package cli_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	var exitErr *cli.ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("campaign compare error = %v, want ExitCodeError", err)
	}
	if exitErr.Code != agentreport.ExitCodeNotConverged {
		t.Fatalf("campaign compare exit code = %d, want %d", exitErr.Code, agentreport.ExitCodeNotConverged)
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
	root.SetArgs([]string{"campaign", "compare", "gpt5.6"})
	err := root.Execute()

	var exitErr *cli.ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("campaign compare error = %v, want ExitCodeError", err)
	}
	if exitErr.Code != agentreport.ExitCodeToolError {
		t.Fatalf("campaign compare exit code = %d, want %d", exitErr.Code, agentreport.ExitCodeToolError)
	}
}
