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
	firstRequestBody      = `{"name":"widget"}`
	firstBaselineBody     = `{"id":"widget","state":"available"}`
	firstCandidateBody    = `{"error":"widget not found"}`
	secondBaselineBody    = `{"error":"baseline unavailable"}`
	secondCandidateBody   = `{"id":"missing","state":"available"}`
	fixedResponseDate     = "Mon, 02 Jan 2006 15:04:05 GMT"
	firstCandidateLength  = "28"
	secondCandidateLength = "36"
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
			} else if len(document.Interactions) != 1 {
				got.Error = fmt.Sprintf(
					"comparison JSON interactions = %d, want 1",
					len(document.Interactions),
				)
			} else {
				interaction := document.Interactions[0]
				got.JSONStatusTransitions = string(document.Summary.StatusTransitions)
				got.JSONBaselineResponse = string(interaction.BaselineResponse)
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
					"### Interaction ",
					"- Candidate target:",
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
		JSONTransitionBaseline:  "null",
		JSONTransitionCandidate: http.StatusOK,
		JSONRequestURL:          "http://baseline.invalid/no-recorded-response",
		JSONTargetURL:           server.URL + "/no-recorded-response",
		JSONCandidateStatus:     http.StatusOK,
		JSONCandidateBody:       "candidate body",
		JSONLatencyMS:           2,
		MarkdownLines: []string{
			"- Exact status transitions: none",
			"### Interaction 1: `GET http://baseline.invalid/no-recorded-response`",
			"- Candidate target: `" + server.URL + "/no-recorded-response`",
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

func expectedComparisonReport(baseURL string) comparisonReport {
	return comparisonReport{
		SchemaVersion: "2",
		Baseline: comparisonCampaign{
			Campaign:           "baseline",
			ProblemCount:       3,
			ProblemCountSource: filepath.Join("reports", "baseline", "junit.xml"),
		},
		Candidate: comparisonCandidate{
			Campaign: "gpt5.6",
			BaseURL:  baseURL,
		},
		Summary: comparisonSummary{
			InteractionCount: 2,
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
		Interactions: []comparisonInteractionEvidence{
			expectedFirstComparisonInteraction(baseURL),
			expectedSecondComparisonInteraction(baseURL),
		},
	}
}

func expectedFirstComparisonInteraction(baseURL string) comparisonInteractionEvidence {
	return comparisonInteractionEvidence{
		Interaction: 1,
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
		Interaction: 2,
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
	Interactions []struct {
		Request struct {
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
	} `json:"interactions"`
}

type comparisonReport struct {
	SchemaVersion            string                          `json:"schema_version"`
	Baseline                 comparisonCampaign              `json:"baseline"`
	Candidate                comparisonCandidate             `json:"candidate"`
	Summary                  comparisonSummary               `json:"summary"`
	BaselineProblemsAvailable bool                            `json:"baseline_problems_available"`
	BaselineProblemsNote      string                          `json:"baseline_problems_note"`
	Interactions              []comparisonInteractionEvidence `json:"interactions"`
}

type comparisonCampaign struct {
	Campaign           string `json:"campaign"`
	ProblemCount       int    `json:"problem_count"`
	ProblemCountSource string `json:"problem_count_source"`
}

type comparisonCandidate struct {
	Campaign string `json:"campaign"`
	BaseURL  string `json:"base_url"`
}

type comparisonSummary struct {
	InteractionCount  int                               `json:"interaction_count"`
	LatencyMS         comparisonLatency                 `json:"latency_ms"`
	StatusTransitions []comparisonStatusTransitionCount `json:"status_transitions"`
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
