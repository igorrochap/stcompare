package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionalEndToEndBenchmarkVerification(t *testing.T) {
	if os.Getenv("STCOMPARE_RUN_E2E_BENCHMARK") != "1" {
		t.Skip("set STCOMPARE_RUN_E2E_BENCHMARK=1 to enable optional end-to-end benchmark verification")
	}
	requireSchemathesis(t)

	binary := buildStcompare(t)
	baseline := newBenchmarkService(false)
	t.Cleanup(baseline.Close)
	candidate := newBenchmarkService(true)
	t.Cleanup(candidate.Close)

	runDir := t.TempDir()
	writeBenchmarkSchema(t, filepath.Join(runDir, "openapi.yaml"))
	writeBenchmarkConfig(t, filepath.Join(runDir, "stcompare.yaml"), baseline.URL)

	runStcompare(t, binary, runDir, "campaign", "run", "baseline")
	runStcompare(t, binary, runDir, "campaign", "run", "candidate", "--base-url", candidate.URL)
	runStcompare(t, binary, runDir, "campaign", "compare", "candidate", "--base-url", candidate.URL)

	assertCampaignCapture(t, runDir, "baseline")
	assertCampaignCapture(t, runDir, "candidate")
	assertSchemathesisVersionRecorded(t, filepath.Join(runDir, "reports", "baseline", "metadata.yaml"))
	assertBenchmarkReportClaims(t, filepath.Join(runDir, "reports", "candidate", "comparison.json"))
}

func requireSchemathesis(t *testing.T) {
	t.Helper()

	fields := strings.Fields(os.Getenv("STCOMPARE_SCHEMATHESIS_COMMAND"))
	if len(fields) == 0 {
		if path, err := exec.LookPath("st"); err == nil {
			fields = []string{path}
		} else if path, err := exec.LookPath("uvx"); err == nil {
			fields = []string{path, "schemathesis"}
		} else {
			t.Skip("Schemathesis prerequisite unavailable: install st or uvx, or set STCOMPARE_SCHEMATHESIS_COMMAND")
		}
	}

	command := exec.Command(fields[0], append(fields[1:], "--version")...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Skipf("Schemathesis prerequisite unavailable: %v\n%s", err, output)
	}
	t.Logf("optional benchmark exercising %s", strings.TrimSpace(string(output)))
}

func buildStcompare(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "stcompare")
	build := exec.Command("go", "build", "-o", binary, "./cmd/stcompare")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stcompare: %v\n%s", err, output)
	}

	return binary
}

func newBenchmarkService(fixed bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/widgets" {
			http.NotFound(response, request)
			return
		}
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		if fixed {
			_, _ = response.Write([]byte(`{"name":"widget"}`))
			return
		}
		_, _ = response.Write([]byte(`{"unexpected":true}`))
	}))
}

func writeBenchmarkSchema(t *testing.T, path string) {
	t.Helper()

	const schema = `openapi: 3.0.3
info:
  title: stcompare benchmark verification fixture
  version: "1.0"
paths:
  /widgets:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required:
                - name
              properties:
                name:
                  type: string
      responses:
        "201":
          description: created widget
          content:
            application/json:
              schema:
                type: object
                required:
                  - name
                properties:
                  name:
                    type: string
`
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatalf("write benchmark schema: %v", err)
	}
}

func writeBenchmarkConfig(t *testing.T, path string, baselineURL string) {
	t.Helper()

	contents := fmt.Sprintf(`schema: openapi.yaml
base_url: %s
reports_dir: reports
schemathesis:
  seed: 12345
  workers: 1
  generation_deterministic: true
  generation_database: none
  reports:
    - junit
    - vcr
    - har
    - ndjson
  output_sanitize: false
  output_truncate: false
  extra_args:
    - --max-examples
    - "3"
comparison:
  missing_resource_statuses:
    - 404
    - 410
  precondition_heuristics: []
  normalization:
    default_rules: true
    body_fields: []
    headers: []
campaigns:
  baseline:
    kind: baseline
  candidate:
    kind: candidate
`, baselineURL)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write benchmark config: %v", err)
	}
}

func runStcompare(t *testing.T, binary string, dir string, args ...string) {
	t.Helper()

	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("stcompare %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	t.Logf("stcompare %s\n%s", strings.Join(args, " "), output)
}

func assertCampaignCapture(t *testing.T, runDir string, campaign string) {
	t.Helper()

	for _, name := range []string{
		"junit.xml",
		"campaign.vcr.yaml",
		"campaign.har.json",
		"campaign.ndjson",
		"metadata.yaml",
	} {
		path := filepath.Join(runDir, "reports", campaign, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s campaign did not capture %s: %v", campaign, name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s campaign captured empty %s", campaign, name)
		}
	}
}

func assertSchemathesisVersionRecorded(t *testing.T, path string) {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read campaign metadata: %v", err)
	}
	if !strings.Contains(string(contents), "schemathesis_version:") {
		t.Fatalf("metadata did not record schemathesis_version:\n%s", contents)
	}
}

func assertBenchmarkReportClaims(t *testing.T, path string) {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read comparison report: %v", err)
	}

	var report benchmarkReport
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("decode comparison report: %v\n%s", err, contents)
	}

	if !report.BaselineProblemsAvailable {
		t.Fatalf("baseline problems were not available: %s", report.BaselineProblemsNote)
	}
	if report.Baseline.ExtractedProblemCount == nil || *report.Baseline.ExtractedProblemCount < 1 {
		t.Fatalf("baseline problems were not extracted: %#v", report.Baseline.ExtractedProblemCount)
	}
	if report.Summary.BaselineProblems.Total < 1 {
		t.Fatalf("report has no baseline problems: %#v", report.Summary.BaselineProblems)
	}
	if report.Summary.BaselineProblems.Evaluable < 1 {
		t.Fatalf("report has no evaluable baseline problems: %#v", report.Summary.BaselineProblems)
	}
	if report.Summary.BaselineProblems.Fixed < 1 {
		t.Fatalf("report has no fixed baseline problem: %#v", report.Summary.BaselineProblems)
	}

	var correlated, withOutcome bool
	for _, problem := range report.Problems {
		if problem.CorrelationStatus == "correlated" {
			correlated = true
		}
		if problem.Outcome != "" {
			withOutcome = true
		}
	}
	if !correlated {
		t.Fatalf("report has no correlated baseline problem: %#v", report.Problems)
	}
	if !withOutcome {
		t.Fatalf("report has no baseline problem outcome: %#v", report.Problems)
	}
}

type benchmarkReport struct {
	Baseline struct {
		ExtractedProblemCount *int `json:"extracted_problem_count"`
	} `json:"baseline"`
	Summary struct {
		BaselineProblems struct {
			Total     int `json:"total"`
			Evaluable int `json:"evaluable"`
			Fixed     int `json:"fixed"`
		} `json:"baseline_problems"`
	} `json:"summary"`
	BaselineProblemsAvailable bool   `json:"baseline_problems_available"`
	BaselineProblemsNote      string `json:"baseline_problems_note"`
	Problems                  []struct {
		CorrelationStatus string `json:"correlation_status"`
		Outcome           string `json:"outcome"`
	} `json:"problems"`
}
