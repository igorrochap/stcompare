package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"stcompare/internal/cli"
)

const (
	firstRequestBody           = `{"name":"widget"}`
	firstBaselineBody          = `{"id":"widget","state":"available"}`
	firstCandidateBody         = `{"error":"widget not found"}`
	secondBaselineBody         = `{"error":"baseline unavailable"}`
	secondCandidateBody        = `{"id":"missing","state":"available"}`
	fixedResponseDate          = "Mon, 02 Jan 2006 15:04:05 GMT"
	firstCandidateLength       = "28"
	secondCandidateLength      = "36"
	problemCountExplanation    = "JUnit reports deduplicated Schemathesis problems. Structured evidence records every failing case from VCR/NDJSON when available, but no structured problem count is available in this report."
	problemBucketExplanation   = "evaluable = fixed + still_failing + inconclusive; total = evaluable + unevaluable + uncorrelated + ambiguous. Every extracted Schemathesis problem is assigned to exactly one bucket. Only evaluable problems receive fixed, still_failing, or evaluable inconclusive counts; unevaluable, uncorrelated, and ambiguous problems carry not_evaluated outcomes with a reason on the problem entry."
	fixRateMeaning             = "Problems fixed among evaluable baseline problems in this comparison. It excludes uncorrelated, ambiguous, and unevaluable baseline problems; counts Schemathesis problems rather than distinct defects; and is comparable only for the same baseline and report schema version."
	fixRateZeroDenominatorNote = "Fix rate is unavailable because there are zero evaluable baseline problems."
)

func TestCampaignCompareWritesCompleteJSONReport(t *testing.T) {
	fixture := newComparisonReportFixture(t)
	server := fixture.Server
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{Now: fixture.Now})
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})

	got := comparisonJSONOutcome{}
	if err := root.Execute(); err != nil {
		got.Error = err.Error()
	} else {
		contents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.json"))
		if err != nil {
			got.Error = err.Error()
		} else if err := json.Unmarshal(contents, &got.Report); err != nil {
			got.Error = err.Error()
		}
	}
	want := comparisonJSONOutcome{
		Report: expectedComparisonReport(server.URL),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare JSON report = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareWritesReadableMarkdownReport(t *testing.T) {
	fixture := newComparisonReportFixture(t)
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{Now: fixture.Now})
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", fixture.Server.URL})

	got := comparisonMarkdownOutcome{}
	if err := root.Execute(); err != nil {
		got.Error = err.Error()
	} else {
		contents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.md"))
		if err != nil {
			got.Error = err.Error()
		} else {
			got.Contents = string(contents)
		}
	}
	want := comparisonMarkdownOutcome{
		Contents: expectedComparisonMarkdownReport(t, fixture.Server.URL),
	}

	if got != want {
		t.Fatalf("campaign compare Markdown report = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareKeepsJSONAndMarkdownInSyncWhenBaselineProblemsAreAvailable(t *testing.T) {
	fixture := newComparisonReportWithProblemsFixture(t)
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{Now: fixture.Now})
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", fixture.Server.URL})

	got := availableBaselineProblemsOutcome{}
	if err := root.Execute(); err != nil {
		got.Error = err.Error()
	} else {
		jsonContents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.json"))
		if err != nil {
			got.Error = err.Error()
		} else {
			var document availableBaselineProblemsJSON
			if err := json.Unmarshal(jsonContents, &document); err != nil {
				got.Error = err.Error()
			} else {
				got.JSONAvailable = document.BaselineProblemsAvailable
				got.JSONNote = document.BaselineProblemsNote
				got.JSONProblemCount = len(document.Problems)
				got.JSONExtractedProblemCount = valueOrZero(document.Baseline.ExtractedProblemCount)
			}
		}
		if got.Error == "" {
			markdownContents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.md"))
			if err != nil {
				got.Error = err.Error()
			} else {
				markdown := string(markdownContents)
				got.MarkdownHasUnavailableDisclosure = strings.Contains(
					markdown,
					"> Baseline Schemathesis problems are unavailable:",
				)
				got.MarkdownHasProblemSection = strings.Contains(
					markdown,
					"## Baseline problems",
				)
			}
		}
	}
	want := availableBaselineProblemsOutcome{
		JSONAvailable:                    true,
		JSONNote:                         "",
		JSONProblemCount:                 1,
		JSONExtractedProblemCount:        1,
		MarkdownHasUnavailableDisclosure: false,
		MarkdownHasProblemSection:        true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare available baseline problems outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareAppliesConfiguredPreconditionPolicyToJSONReport(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	configSection(t, defaultConfig, "comparison")["precondition_heuristics"] = []any{
		map[string]any{
			"name":         "generated-widget",
			"method":       "GET",
			"path_pattern": `^/widgets/[0-9a-f]+$`,
		},
	}
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/widgets/a8f31?expand=owner",
			Headers: []harHeaderFixture{
				{Name: "X-Schemathesis-TestCaseId", Value: "case-42"},
			},
			ResponseStatus: http.StatusOK,
		},
	})
	writeBaselinePreconditionVCR(
		t,
		filepath.Join("reports", "baseline", "campaign.vcr.yaml"),
	)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	clockValues := []time.Time{
		time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 5, int(4*time.Millisecond), time.UTC),
	}
	clockIndex := 0
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{
		Now: func() time.Time {
			value := clockValues[clockIndex]
			clockIndex++
			return value
		},
	})
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})

	got := preconditionPolicyOutcome{}
	if err := root.Execute(); err != nil {
		got.Error = err.Error()
	} else {
		contents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.json"))
		if err != nil {
			got.Error = err.Error()
		} else {
			var document preconditionPolicyJSON
			if err := json.Unmarshal(contents, &document); err != nil {
				got.Error = err.Error()
			} else {
				got.ProblemCount = len(document.Problems)
				got.Evaluable = document.Summary.BaselineProblems.Evaluable
				got.Inconclusive = document.Summary.BaselineProblems.Inconclusive
				if len(document.Problems) == 1 {
					got.Outcome = document.Problems[0].Outcome
					got.OutcomeReason = document.Problems[0].OutcomeReason
					got.MatchedPreconditionHeuristic =
						document.Problems[0].MatchedPreconditionHeuristic
				}
			}
		}
	}
	want := preconditionPolicyOutcome{
		ProblemCount:                 1,
		Outcome:                      "inconclusive",
		OutcomeReason:                "generated_resource_precondition_loss",
		MatchedPreconditionHeuristic: "generated-widget",
		Evaluable:                    1,
		Inconclusive:                 1,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare precondition policy outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareReportsUnknownBaselineProblemsWithoutJUnit(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method:         "GET",
			URL:            "http://baseline.invalid/unavailable",
			ResponseStatus: http.StatusServiceUnavailable,
		},
	})

	var (
		mu                    sync.Mutex
		candidateRequestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		candidateRequestCount++
		mu.Unlock()
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	clockValues := []time.Time{
		time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 5, int(3*time.Millisecond), time.UTC),
	}
	clockIndex := 0
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{
		Now: func() time.Time {
			value := clockValues[clockIndex]
			clockIndex++
			return value
		},
	})
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})

	got := unknownBaselineProblemsOutcome{}
	if err := root.Execute(); err != nil {
		got.Error = err.Error()
	} else {
		jsonContents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.json"))
		if err != nil {
			got.Error = err.Error()
		} else {
			var document unknownBaselineProblemsJSON
			if err := json.Unmarshal(jsonContents, &document); err != nil {
				got.Error = err.Error()
			} else {
				got.JSONProblemCount = string(document.Baseline.ProblemCount)
				got.JSONProblemCountSource = string(document.Baseline.ProblemCountSource)
			}
		}
		if got.Error == "" {
			markdownContents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.md"))
			if err != nil {
				got.Error = err.Error()
			} else {
				for _, line := range strings.Split(string(markdownContents), "\n") {
					if strings.HasPrefix(line, "- Baseline problems:") {
						got.MarkdownBaselineProblems = line
						break
					}
				}
			}
		}
	}
	mu.Lock()
	got.CandidateRequestCount = candidateRequestCount
	mu.Unlock()
	want := unknownBaselineProblemsOutcome{
		CandidateRequestCount:    1,
		JSONProblemCount:         "null",
		JSONProblemCountSource:   "null",
		MarkdownBaselineProblems: "- Baseline problems: unknown",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare missing JUnit report outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareReportsUnrecordedBaselineResponseAsUnknown(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/no-recorded-response",
		},
	})

	var (
		mu                    sync.Mutex
		candidateRequestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		candidateRequestCount++
		mu.Unlock()
		response.Header().Set("X-Candidate", "recorded")
		response.WriteHeader(http.StatusOK)
		if _, err := response.Write([]byte("candidate body")); err != nil {
			t.Fatalf("write candidate response: %v", err)
		}
	}))
	defer server.Close()

	clockValues := []time.Time{
		time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 5, int(2*time.Millisecond), time.UTC),
	}
	clockIndex := 0
	root := cli.NewRootCommandWithDependencies(cli.Dependencies{
		Now: func() time.Time {
			value := clockValues[clockIndex]
			clockIndex++
			return value
		},
	})
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})

	got := unrecordedBaselineResponseOutcome{}
	if err := root.Execute(); err != nil {
		got.Error = err.Error()
	} else {
		jsonContents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.json"))
		if err != nil {
			got.Error = err.Error()
		} else {
			var document unrecordedBaselineResponseJSON
			if err := json.Unmarshal(jsonContents, &document); err != nil {
				got.Error = err.Error()
			} else if len(document.Findings) != 1 {
				got.Error = fmt.Sprintf(
					"comparison JSON findings = %d, want 1",
					len(document.Findings),
				)
			} else {
				interaction := document.Findings[0]
				got.JSONStatusTransitions = string(document.Summary.StatusTransitions)
				got.JSONBaselineResponse = string(interaction.BaselineResponse)
				got.JSONClassification = interaction.Classification
				got.JSONTransitionBaseline = string(interaction.StatusTransition.Baseline)
				got.JSONTransitionCandidate = interaction.StatusTransition.Candidate
				got.JSONRequestURL = interaction.Request.URL
				got.JSONTargetURL = interaction.TargetURL
				got.JSONCandidateStatus = interaction.CandidateResponse.Status
				got.JSONCandidateBody = interaction.CandidateResponse.Body
				got.JSONLatencyMS = interaction.LatencyMS
			}
		}
		if got.Error == "" {
			markdownContents, err := os.ReadFile(filepath.Join("reports", "gpt5.6", "comparison.md"))
			if err != nil {
				got.Error = err.Error()
			} else {
				focusedPrefixes := []string{
					"- Exact status transitions:",
					"### Finding ",
					"- Candidate target:",
					"- Classification:",
					"- Latency:",
					"- Status transition:",
					"#### Baseline response:",
					"#### Candidate response:",
				}
				for _, line := range strings.Split(string(markdownContents), "\n") {
					for _, prefix := range focusedPrefixes {
						if strings.HasPrefix(line, prefix) {
							got.MarkdownLines = append(got.MarkdownLines, line)
							break
						}
					}
				}
			}
		}
	}
	mu.Lock()
	got.CandidateRequestCount = candidateRequestCount
	mu.Unlock()
	want := unrecordedBaselineResponseOutcome{
		CandidateRequestCount:   1,
		JSONStatusTransitions:   "[]",
		JSONBaselineResponse:    "null",
		JSONClassification:      "changed",
		JSONTransitionBaseline:  "null",
		JSONTransitionCandidate: http.StatusOK,
		JSONRequestURL:          "http://baseline.invalid/no-recorded-response",
		JSONTargetURL:           server.URL + "/no-recorded-response",
		JSONCandidateStatus:     http.StatusOK,
		JSONCandidateBody:       "candidate body",
		JSONLatencyMS:           2,
		MarkdownLines: []string{
			"- Exact status transitions: none",
			"### Finding 1: `GET http://baseline.invalid/no-recorded-response`",
			"- Candidate target: `" + server.URL + "/no-recorded-response`",
			"- Classification: `changed`",
			"- Latency: 2 ms",
			"- Status transition: `unknown -> 200`",
			"#### Baseline response: unknown",
			"#### Candidate response: `200`",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare unrecorded baseline response outcome = %#v, want %#v", got, want)
	}
}

func newComparisonReportFixture(t *testing.T) comparisonReportFixture {
	t.Helper()

	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "POST",
			URL:    "http://baseline.invalid/widgets?dryRun=true",
			Headers: []harHeaderFixture{
				{Name: "Z-Request", Value: "last"},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "A-Request", Value: "first"},
			},
			PostDataText:   firstRequestBody,
			ResponseStatus: http.StatusOK,
			ResponseHeaders: []harHeaderFixture{
				{Name: "X-Baseline-Z", Value: "last"},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "X-Baseline-A", Value: "first"},
			},
			ResponseBody: firstBaselineBody,
		},
		{
			Method: "GET",
			URL:    "http://baseline.invalid/missing",
			Headers: []harHeaderFixture{
				{Name: "Z-Request", Value: "last"},
				{Name: "A-Request", Value: "first"},
			},
			ResponseStatus: http.StatusInternalServerError,
			ResponseHeaders: []harHeaderFixture{
				{Name: "X-Baseline-Z", Value: "last"},
				{Name: "X-Baseline-A", Value: "first"},
			},
			ResponseBody: secondBaselineBody,
		},
	})
	writeBaselineJUnit(t, filepath.Join("reports", "baseline", "junit.xml"))

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Date", fixedResponseDate)
		response.Header().Set("X-Candidate-Z", "last")
		response.Header().Set("X-Candidate-A", "first")
		switch request.URL.Path {
		case "/widgets":
			response.Header().Set("Content-Type", "application/problem+json")
			response.Header().Set("Content-Length", firstCandidateLength)
			response.WriteHeader(http.StatusNotFound)
			if _, err := response.Write([]byte(firstCandidateBody)); err != nil {
				t.Fatalf("write first candidate response: %v", err)
			}
		case "/missing":
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Content-Length", secondCandidateLength)
			response.WriteHeader(http.StatusOK)
			if _, err := response.Write([]byte(secondCandidateBody)); err != nil {
				t.Fatalf("write second candidate response: %v", err)
			}
		default:
			t.Fatalf("unexpected candidate request path %q", request.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	clockValues := []time.Time{
		time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 5, int(4*time.Millisecond), time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 6, 0, time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 6, int(10*time.Millisecond), time.UTC),
	}
	clockIndex := 0
	return comparisonReportFixture{
		Server: server,
		Now: func() time.Time {
			value := clockValues[clockIndex]
			clockIndex++
			return value
		},
	}
}

func newComparisonReportWithProblemsFixture(t *testing.T) comparisonReportFixture {
	t.Helper()

	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "POST",
			URL:    "http://baseline.invalid/widgets?dryRun=true",
			Headers: []harHeaderFixture{
				{Name: "X-Schemathesis-TestCaseId", Value: "case-42"},
				{Name: "Content-Type", Value: "application/json"},
			},
			PostDataText:   firstRequestBody,
			ResponseStatus: http.StatusOK,
			ResponseHeaders: []harHeaderFixture{
				{Name: "Content-Type", Value: "application/json"},
			},
			ResponseBody: firstBaselineBody,
		},
	})
	writeBaselineVCR(t, filepath.Join("reports", "baseline", "campaign.vcr.yaml"))

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Date", fixedResponseDate)
		response.Header().Set("Content-Type", "application/problem+json")
		response.Header().Set("Content-Length", firstCandidateLength)
		response.WriteHeader(http.StatusNotFound)
		if _, err := response.Write([]byte(firstCandidateBody)); err != nil {
			t.Fatalf("write candidate response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	clockValues := []time.Time{
		time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, time.January, 2, 3, 4, 5, int(4*time.Millisecond), time.UTC),
	}
	clockIndex := 0
	return comparisonReportFixture{
		Server: server,
		Now: func() time.Time {
			value := clockValues[clockIndex]
			clockIndex++
			return value
		},
	}
}

func writeBaselinePreconditionVCR(t *testing.T, path string) {
	t.Helper()

	const document = `
http_interactions:
  - id: case-42
    checks:
      - name: response_schema_conformance
        status: FAILURE
        message: "Response violates schema"
    request:
      uri: "http://baseline.invalid/widgets/a8f31?expand=owner"
      method: GET
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create baseline VCR directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write baseline VCR: %v", err)
	}
}

func expectedComparisonReport(baseURL string) comparisonReport {
	return comparisonReport{
		SchemaVersion: "10",
		Baseline: comparisonCampaign{
			Campaign:           "baseline",
			ProblemCount:       3,
			ProblemCountSource: filepath.Join("reports", "baseline", "junit.xml"),
		},
		Candidate: comparisonCandidate{
			Campaign: "gpt5.6",
			BaseURL:  baseURL,
		},
		Explanations: comparisonExplanations{
			BaselineProblemCounts:  problemCountExplanation,
			BaselineProblemBuckets: problemBucketExplanation,
		},
		Summary: comparisonSummary{
			InteractionCount: 2,
			BaselineProblems: comparisonBaselineProblemSummary{
				FixRate: comparisonBaselineProblemFixRate{
					DenominatorBasis: "evaluable_baseline_problems",
					Meaning:          fixRateMeaning,
					Note:             fixRateZeroDenominatorNote,
				},
			},
			Traffic: comparisonTrafficSummary{
				Total:   2,
				Changed: 2,
			},
			LatencyMS: comparisonLatency{
				Minimum: 4,
				Maximum: 10,
				Average: 7,
			},
			StatusTransitions: []comparisonStatusTransitionCount{
				{Baseline: http.StatusOK, Candidate: http.StatusNotFound, Count: 1},
				{Baseline: http.StatusInternalServerError, Candidate: http.StatusOK, Count: 1},
			},
		},
		BaselineProblemsAvailable: false,
		BaselineProblemsNote: "Baseline Schemathesis problems could not be " +
			"extracted from structured evidence.",
		Findings: []comparisonInteractionEvidence{
			expectedFirstComparisonInteraction(baseURL),
			expectedSecondComparisonInteraction(baseURL),
		},
	}
}

func expectedFirstComparisonInteraction(baseURL string) comparisonInteractionEvidence {
	return comparisonInteractionEvidence{
		Interaction:    1,
		Classification: "changed",
		Request: comparisonRequest{
			Method: "POST",
			URL:    "http://baseline.invalid/widgets?dryRun=true",
			Headers: []harHeaderFixture{
				{Name: "A-Request", Value: "first"},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "Z-Request", Value: "last"},
			},
			Body: firstRequestBody,
		},
		TargetURL: baseURL + "/widgets?dryRun=true",
		BaselineResponse: comparisonResponse{
			Status: http.StatusOK,
			Headers: []harHeaderFixture{
				{Name: "Content-Type", Value: "application/json"},
				{Name: "X-Baseline-A", Value: "first"},
				{Name: "X-Baseline-Z", Value: "last"},
			},
			Body: firstBaselineBody,
		},
		CandidateResponse: comparisonResponse{
			Status: http.StatusNotFound,
			Headers: []harHeaderFixture{
				{Name: "Content-Length", Value: firstCandidateLength},
				{Name: "Content-Type", Value: "application/problem+json"},
				{Name: "Date", Value: fixedResponseDate},
				{Name: "X-Candidate-A", Value: "first"},
				{Name: "X-Candidate-Z", Value: "last"},
			},
			Body: firstCandidateBody,
		},
		LatencyMS: 4,
		StatusTransition: comparisonStatusTransition{
			Baseline:  http.StatusOK,
			Candidate: http.StatusNotFound,
		},
	}
}

func expectedSecondComparisonInteraction(baseURL string) comparisonInteractionEvidence {
	return comparisonInteractionEvidence{
		Interaction:    2,
		Classification: "changed",
		Request: comparisonRequest{
			Method: "GET",
			URL:    "http://baseline.invalid/missing",
			Headers: []harHeaderFixture{
				{Name: "A-Request", Value: "first"},
				{Name: "Z-Request", Value: "last"},
			},
			Body: "",
		},
		TargetURL: baseURL + "/missing",
		BaselineResponse: comparisonResponse{
			Status: http.StatusInternalServerError,
			Headers: []harHeaderFixture{
				{Name: "X-Baseline-A", Value: "first"},
				{Name: "X-Baseline-Z", Value: "last"},
			},
			Body: secondBaselineBody,
		},
		CandidateResponse: comparisonResponse{
			Status: http.StatusOK,
			Headers: []harHeaderFixture{
				{Name: "Content-Length", Value: secondCandidateLength},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "Date", Value: fixedResponseDate},
				{Name: "X-Candidate-A", Value: "first"},
				{Name: "X-Candidate-Z", Value: "last"},
			},
			Body: secondCandidateBody,
		},
		LatencyMS: 10,
		StatusTransition: comparisonStatusTransition{
			Baseline:  http.StatusInternalServerError,
			Candidate: http.StatusOK,
		},
	}
}

func expectedComparisonMarkdownReport(t *testing.T, baseURL string) string {
	t.Helper()

	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate expected Markdown report fixture")
	}
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(sourcePath), "testdata", "campaign_compare_report.md"))
	if err != nil {
		t.Fatalf("read expected Markdown report: %v", err)
	}

	return fmt.Sprintf(string(contents), baseURL)
}

type comparisonReportFixture struct {
	Server *httptest.Server
	Now    func() time.Time
}

type comparisonJSONOutcome struct {
	Error  string
	Report comparisonReport
}

type comparisonMarkdownOutcome struct {
	Error    string
	Contents string
}

type availableBaselineProblemsOutcome struct {
	Error                            string
	JSONAvailable                    bool
	JSONNote                         string
	JSONProblemCount                 int
	JSONExtractedProblemCount        int
	MarkdownHasUnavailableDisclosure bool
	MarkdownHasProblemSection        bool
}

type availableBaselineProblemsJSON struct {
	BaselineProblemsAvailable bool   `json:"baseline_problems_available"`
	BaselineProblemsNote      string `json:"baseline_problems_note"`
	Baseline                  struct {
		ExtractedProblemCount *int `json:"extracted_problem_count"`
	} `json:"baseline"`
	Problems []struct {
		CaseID      string `json:"case_id"`
		Interaction *int   `json:"interaction"`
	} `json:"problems"`
}

type preconditionPolicyOutcome struct {
	Error                        string
	ProblemCount                 int
	Outcome                      string
	OutcomeReason                string
	MatchedPreconditionHeuristic string
	Evaluable                    int
	Inconclusive                 int
}

type preconditionPolicyJSON struct {
	Summary struct {
		BaselineProblems struct {
			Evaluable    int `json:"evaluable"`
			Inconclusive int `json:"inconclusive"`
		} `json:"baseline_problems"`
	} `json:"summary"`
	Problems []struct {
		Outcome                      string `json:"outcome"`
		OutcomeReason                string `json:"outcome_reason"`
		MatchedPreconditionHeuristic string `json:"matched_precondition_heuristic"`
	} `json:"problems"`
}

type unknownBaselineProblemsOutcome struct {
	Error                    string
	CandidateRequestCount    int
	JSONProblemCount         string
	JSONProblemCountSource   string
	MarkdownBaselineProblems string
}

type unknownBaselineProblemsJSON struct {
	Baseline struct {
		ProblemCount       json.RawMessage `json:"problem_count"`
		ProblemCountSource json.RawMessage `json:"problem_count_source"`
	} `json:"baseline"`
}

type unrecordedBaselineResponseOutcome struct {
	Error                   string
	CandidateRequestCount   int
	JSONStatusTransitions   string
	JSONBaselineResponse    string
	JSONClassification      string
	JSONTransitionBaseline  string
	JSONTransitionCandidate int
	JSONRequestURL          string
	JSONTargetURL           string
	JSONCandidateStatus     int
	JSONCandidateBody       string
	JSONLatencyMS           int
	MarkdownLines           []string
}

type unrecordedBaselineResponseJSON struct {
	Summary struct {
		StatusTransitions json.RawMessage `json:"status_transitions"`
	} `json:"summary"`
	Findings []struct {
		Classification string `json:"classification"`
		Request        struct {
			URL string `json:"url"`
		} `json:"request"`
		TargetURL         string          `json:"target_url"`
		BaselineResponse  json.RawMessage `json:"baseline_response"`
		CandidateResponse struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		} `json:"candidate_response"`
		LatencyMS        int `json:"latency_ms"`
		StatusTransition struct {
			Baseline  json.RawMessage `json:"baseline"`
			Candidate int             `json:"candidate"`
		} `json:"status_transition"`
	} `json:"findings"`
}

type comparisonReport struct {
	SchemaVersion             string                          `json:"schema_version"`
	Baseline                  comparisonCampaign              `json:"baseline"`
	Candidate                 comparisonCandidate             `json:"candidate"`
	Explanations              comparisonExplanations          `json:"explanations"`
	Summary                   comparisonSummary               `json:"summary"`
	BaselineProblemsAvailable bool                            `json:"baseline_problems_available"`
	BaselineProblemsNote      string                          `json:"baseline_problems_note"`
	Findings                  []comparisonInteractionEvidence `json:"findings"`
}

type comparisonExplanations struct {
	BaselineProblemCounts  string `json:"baseline_problem_counts"`
	BaselineProblemBuckets string `json:"baseline_problem_buckets"`
}

type comparisonCampaign struct {
	Campaign                    string  `json:"campaign"`
	ProblemCount                int     `json:"problem_count"`
	ProblemCountSource          string  `json:"problem_count_source"`
	ExtractedProblemCount       *int    `json:"extracted_problem_count"`
	ExtractedProblemCountSource *string `json:"extracted_problem_count_source"`
}

type comparisonCandidate struct {
	Campaign string `json:"campaign"`
	BaseURL  string `json:"base_url"`
}

type comparisonSummary struct {
	InteractionCount  int                               `json:"interaction_count"`
	BaselineProblems  comparisonBaselineProblemSummary  `json:"baseline_problems"`
	Traffic           comparisonTrafficSummary          `json:"traffic"`
	LatencyMS         comparisonLatency                 `json:"latency_ms"`
	StatusTransitions []comparisonStatusTransitionCount `json:"status_transitions"`
}

type comparisonBaselineProblemSummary struct {
	Total        int                              `json:"total"`
	Evaluable    int                              `json:"evaluable"`
	Unevaluable  int                              `json:"unevaluable"`
	Uncorrelated int                              `json:"uncorrelated"`
	Ambiguous    int                              `json:"ambiguous"`
	Fixed        int                              `json:"fixed"`
	StillFailing int                              `json:"still_failing"`
	Inconclusive int                              `json:"inconclusive"`
	FixRate      comparisonBaselineProblemFixRate `json:"fix_rate"`
}

type comparisonBaselineProblemFixRate struct {
	Available        bool     `json:"available"`
	Fixed            int      `json:"fixed"`
	Denominator      int      `json:"denominator"`
	DenominatorBasis string   `json:"denominator_basis"`
	Percentage       *float64 `json:"percentage"`
	Meaning          string   `json:"meaning"`
	Note             string   `json:"note,omitempty"`
}

type comparisonTrafficSummary struct {
	Total            int `json:"total"`
	SuccessUnchanged int `json:"success_unchanged"`
	Changed          int `json:"changed"`
	Regressed        int `json:"regressed"`
}

type comparisonLatency struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
	Average int `json:"average"`
}

type comparisonStatusTransitionCount struct {
	Baseline  int `json:"baseline"`
	Candidate int `json:"candidate"`
	Count     int `json:"count"`
}

type comparisonInteractionEvidence struct {
	Interaction       int                        `json:"interaction"`
	Classification    string                     `json:"classification"`
	Request           comparisonRequest          `json:"request"`
	TargetURL         string                     `json:"target_url"`
	BaselineResponse  comparisonResponse         `json:"baseline_response"`
	CandidateResponse comparisonResponse         `json:"candidate_response"`
	LatencyMS         int                        `json:"latency_ms"`
	StatusTransition  comparisonStatusTransition `json:"status_transition"`
}

type comparisonRequest struct {
	Method  string             `json:"method"`
	URL     string             `json:"url"`
	Headers []harHeaderFixture `json:"headers"`
	Body    string             `json:"body"`
}

type comparisonResponse struct {
	Status  int                `json:"status"`
	Headers []harHeaderFixture `json:"headers"`
	Body    string             `json:"body"`
}

type comparisonStatusTransition struct {
	Baseline  int `json:"baseline"`
	Candidate int `json:"candidate"`
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}

	return *value
}
