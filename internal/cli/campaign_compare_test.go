package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"stcompare/internal/cli"
)

func TestCampaignCompareDiscoversCustomNamedBaselineBeforeSideEffects(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	campaigns := configSection(t, defaultConfig, "campaigns")
	delete(campaigns, "baseline")
	campaigns["reference-run"] = map[string]any{"kind": "baseline"}
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	var (
		mu           sync.Mutex
		requestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	mu.Lock()
	observedRequestCount := requestCount
	mu.Unlock()
	_, statErr := os.Stat(filepath.Join("reports", "gpt5.6"))
	got := struct {
		Error                    string
		CandidateRequestCount    int
		CandidateReportDirExists bool
	}{
		CandidateRequestCount:    observedRequestCount,
		CandidateReportDirExists: !os.IsNotExist(statErr),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error                    string
		CandidateRequestCount    int
		CandidateReportDirExists bool
	}{
		Error: "baseline replay setup: read baseline HAR: open reports/reference-run/campaign.har.json: no such file or directory",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare missing baseline HAR outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareRejectsUnsafeUnselectedCampaignName(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	campaigns := configSection(t, defaultConfig, "campaigns")
	delete(campaigns, "sonnet5")
	campaigns["../escape"] = map[string]any{"kind": "candidate"}
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6"})
	err := root.Execute()

	_, statErr := os.Stat(filepath.Join("reports", "gpt5.6"))
	got := struct {
		Error                    string
		CandidateReportDirExists bool
	}{
		CandidateReportDirExists: !os.IsNotExist(statErr),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error                    string
		CandidateReportDirExists bool
	}{
		Error: `campaign name "../escape" is invalid: use letters, numbers, dots, underscores, or hyphens`,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare unsafe configured campaign outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareRejectsUnsupportedBodyEncodingBeforeSideEffects(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/first",
		},
		{
			Method:           "POST",
			URL:              "http://baseline.invalid/second",
			PostDataText:     "ZW5jb2RlZA==",
			PostDataEncoding: "base64",
		},
	})

	var (
		mu           sync.Mutex
		requestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	mu.Lock()
	observedRequestCount := requestCount
	mu.Unlock()
	_, statErr := os.Stat(filepath.Join("reports", "gpt5.6"))
	got := struct {
		Error                    string
		CandidateRequestCount    int
		CandidateReportDirExists bool
	}{
		CandidateRequestCount:    observedRequestCount,
		CandidateReportDirExists: !os.IsNotExist(statErr),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error                    string
		CandidateRequestCount    int
		CandidateReportDirExists bool
	}{
		Error: `baseline replay setup: request 2 postData encoding "base64" is unsupported`,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare unsupported body encoding outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareRequiresExactlyOneBaselineCampaign(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(configDocument)
		baselineCount int
	}{
		{
			name: "zero",
			mutate: func(document configDocument) {
				delete(configSection(t, document, "campaigns"), "baseline")
			},
			baselineCount: 0,
		},
		{
			name: "multiple",
			mutate: func(document configDocument) {
				configSection(t, document, "campaigns")["reference-run"] = map[string]any{"kind": "baseline"}
			},
			baselineCount: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := loadDefaultConfig(t)
			test.mutate(input)
			t.Chdir(t.TempDir())
			writeConfig(t, "stcompare.yaml", input)

			var (
				mu           sync.Mutex
				requestCount int
			)
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				requestCount++
			}))
			defer server.Close()

			root := cli.NewRootCommand()
			root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
			err := root.Execute()

			mu.Lock()
			observedRequestCount := requestCount
			mu.Unlock()
			_, statErr := os.Stat(filepath.Join("reports", "gpt5.6"))
			got := struct {
				Error                    string
				CandidateRequestCount    int
				CandidateReportDirExists bool
			}{
				CandidateRequestCount:    observedRequestCount,
				CandidateReportDirExists: !os.IsNotExist(statErr),
			}
			if err != nil {
				got.Error = err.Error()
			}
			want := struct {
				Error                    string
				CandidateRequestCount    int
				CandidateReportDirExists bool
			}{
				Error: fmt.Sprintf(
					"baseline replay setup: expected exactly one baseline campaign, found %d",
					test.baselineCount,
				),
			}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("campaign compare baseline cardinality outcome = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCampaignCompareRejectsBaselineAsCandidate(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)

	var (
		mu           sync.Mutex
		requestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "baseline", "--base-url", server.URL})
	err := root.Execute()

	mu.Lock()
	observedRequestCount := requestCount
	mu.Unlock()
	_, statErr := os.Stat(filepath.Join("reports", "baseline"))
	got := struct {
		Error                    string
		CandidateRequestCount    int
		CandidateReportDirExists bool
	}{
		CandidateRequestCount:    observedRequestCount,
		CandidateReportDirExists: !os.IsNotExist(statErr),
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error                    string
		CandidateRequestCount    int
		CandidateReportDirExists bool
	}{
		Error: `campaign "baseline" has kind "baseline": compare requires a candidate campaign`,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare baseline candidate outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareClassifiesCandidateTransportFailure(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/probe",
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	candidateURL := server.URL
	server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", candidateURL})
	err := root.Execute()

	_, statErr := os.Stat(filepath.Join("reports", "gpt5.6"))
	got := struct {
		ErrorHasCandidateAPIPrefix  bool
		ErrorHasBaselineSetupPrefix bool
		CandidateReportDirExists    bool
	}{
		CandidateReportDirExists: !os.IsNotExist(statErr),
	}
	if err != nil {
		got.ErrorHasCandidateAPIPrefix = strings.HasPrefix(
			err.Error(),
			"candidate API: send replay request 1:",
		)
		got.ErrorHasBaselineSetupPrefix = strings.HasPrefix(
			err.Error(),
			"baseline replay setup:",
		)
	}
	want := struct {
		ErrorHasCandidateAPIPrefix  bool
		ErrorHasBaselineSetupPrefix bool
		CandidateReportDirExists    bool
	}{
		ErrorHasCandidateAPIPrefix: true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare candidate transport failure outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignComparePreflightsMalformedRequestsAsBaselineSetup(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/first",
		},
		{
			Method: "BAD METHOD",
			URL:    "http://baseline.invalid/second",
		},
	})

	var (
		mu           sync.Mutex
		requestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	mu.Lock()
	observedRequestCount := requestCount
	mu.Unlock()
	_, statErr := os.Stat(filepath.Join("reports", "gpt5.6"))
	got := struct {
		ErrorHasSetupRequestPrefix bool
		ErrorHasCandidateAPIPrefix bool
		CandidateRequestCount      int
		CandidateReportDirExists   bool
	}{
		CandidateRequestCount:    observedRequestCount,
		CandidateReportDirExists: !os.IsNotExist(statErr),
	}
	if err != nil {
		got.ErrorHasSetupRequestPrefix = strings.HasPrefix(
			err.Error(),
			"baseline replay setup: create replay request 2:",
		)
		got.ErrorHasCandidateAPIPrefix = strings.HasPrefix(err.Error(), "candidate API:")
	}
	want := struct {
		ErrorHasSetupRequestPrefix bool
		ErrorHasCandidateAPIPrefix bool
		CandidateRequestCount      int
		CandidateReportDirExists   bool
	}{
		ErrorHasSetupRequestPrefix: true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare malformed baseline request outcome = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareReplaysHARRequestsToCandidateBaseURLInOrder(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/api%2Fitems/one%20two?tag=a%2Fb&tag=c%20d",
		},
		{
			Method: "GET",
			URL:    "http://baseline.invalid/v1/%7Euser?empty=&encoded=%252F",
		},
	})

	var (
		mu       sync.Mutex
		observed []receivedRequestTarget
	)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, receivedRequestTarget{
			Host:       request.Host,
			RequestURI: request.URL.RequestURI(),
		})
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	host := serverHost(t, server.URL)
	got := struct {
		Error    string
		Observed []receivedRequestTarget
	}{Observed: observed}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Observed []receivedRequestTarget
	}{
		Observed: []receivedRequestTarget{
			{Host: host, RequestURI: "/api%2Fitems/one%20two?tag=a%2Fb&tag=c%20d"},
			{Host: host, RequestURI: "/v1/%7Euser?empty=&encoded=%252F"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare replayed requests = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareReplaysSemanticHeadersAndTextBodyWithoutStaleTransportHeaders(t *testing.T) {
	const body = `{"name":"one two","path":"/api%2Fitems"}`
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "POST",
			URL:    "http://baseline.invalid/api/widgets?encoded=%2F",
			Headers: []harHeaderFixture{
				{Name: "Accept", Value: "application/json"},
				{Name: "Content-Type", Value: "application/json; charset=utf-8"},
				{Name: "Authorization", Value: "Bearer recorded-token"},
				{Name: "X-Correlation-ID", Value: "replay-123"},
				{Name: "Host", Value: "baseline.invalid"},
				{Name: "Content-Length", Value: "999"},
				{Name: "Transfer-Encoding", Value: "chunked"},
				{Name: "Connection", Value: "keep-alive"},
				{Name: "Accept-Encoding", Value: "br"},
			},
			PostDataText: body,
		},
	})

	var observed receivedSemanticRequest
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		bodyContents, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		observed = receivedSemanticRequest{
			Method:           request.Method,
			RequestURI:       request.URL.RequestURI(),
			Host:             request.Host,
			Accept:           request.Header.Get("Accept"),
			ContentType:      request.Header.Get("Content-Type"),
			Authorization:    request.Header.Get("Authorization"),
			CorrelationID:    request.Header.Get("X-Correlation-ID"),
			ContentLength:    request.ContentLength,
			TransferEncoding: request.TransferEncoding,
			Connection:       request.Header.Get("Connection"),
			AcceptEncoding:   request.Header.Get("Accept-Encoding"),
			Body:             string(bodyContents),
		}
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	got := struct {
		Error    string
		Observed receivedSemanticRequest
	}{Observed: observed}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Observed receivedSemanticRequest
	}{
		Observed: receivedSemanticRequest{
			Method:           "POST",
			RequestURI:       "/api/widgets?encoded=%2F",
			Host:             serverHost(t, server.URL),
			Accept:           "application/json",
			ContentType:      "application/json; charset=utf-8",
			Authorization:    "Bearer recorded-token",
			CorrelationID:    "replay-123",
			ContentLength:    int64(len(body)),
			TransferEncoding: nil,
			Connection:       "",
			AcceptEncoding:   "",
			Body:             body,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare replayed semantic request = %#v, want %#v", got, want)
	}
}

func TestCampaignCompareWritesCandidateResponseLog(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/probe",
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-Replay-Marker", "candidate-response")
		response.WriteHeader(http.StatusNonAuthoritativeInfo)
		if _, err := response.Write([]byte("known candidate body")); err != nil {
			t.Fatalf("write response body: %v", err)
		}
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	got := struct {
		Error    string
		Response recordedResponse
	}{}
	if err != nil {
		got.Error = err.Error()
	} else {
		logContents, readErr := os.ReadFile(filepath.Join("reports", "gpt5.6", "replay.har.json"))
		if readErr != nil {
			got.Error = readErr.Error()
		} else {
			var document responseLogDocument
			if decodeErr := json.Unmarshal(logContents, &document); decodeErr != nil {
				got.Error = decodeErr.Error()
			} else {
				response := document.Log.Entries[0].Response
				got.Response = recordedResponse{
					Status: response.Status,
					Marker: responseLogHeaderValue(
						response.Headers,
						"X-Replay-Marker",
					),
					Body: response.Content.Text,
				}
			}
		}
	}
	want := struct {
		Error    string
		Response recordedResponse
	}{
		Response: recordedResponse{
			Status: http.StatusNonAuthoritativeInfo,
			Marker: "candidate-response",
			Body:   "known candidate body",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare response log = %#v, want %#v", got, want)
	}
}

func TestCampaignComparePrintsReplaySummary(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/first",
		},
		{
			Method: "GET",
			URL:    "http://baseline.invalid/second",
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	got := configCommandOutcome{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	}
	want := configCommandOutcome{
		Output: "replayed 2 baseline interactions\n" +
			"wrote reports/gpt5.6/replay.har.json\n" +
			"wrote reports/gpt5.6/comparison.json\n" +
			"wrote reports/gpt5.6/comparison.md\n",
	}

	if got != want {
		t.Fatalf("campaign compare summary = %#v, want %#v", got, want)
	}
}

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
		Report: comparisonReport{
			SchemaVersion: "1",
			Baseline: comparisonCampaign{
				Campaign:           "baseline",
				ProblemCount:       3,
				ProblemCountSource: filepath.Join("reports", "baseline", "junit.xml"),
			},
			Candidate: comparisonCandidate{
				Campaign: "gpt5.6",
				BaseURL:  server.URL,
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
			Findings: []comparisonFinding{
				{
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
					TargetURL: server.URL + "/widgets?dryRun=true",
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
				},
				{
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
					TargetURL: server.URL + "/missing",
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
				},
			},
		},
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
		Contents: fmt.Sprintf(`# Campaign comparison

## Summary

- Total interactions: 2
- Baseline problems: 3 (source: `+"`reports/baseline/junit.xml`"+`)
- Candidate latency: minimum 4 ms, maximum 10 ms, average 7 ms
- Exact status transitions:
  - `+"`200 -> 404`"+`: 1
  - `+"`500 -> 200`"+`: 1
- Baseline campaign: `+"`baseline`"+`
- Candidate campaign: `+"`gpt5.6`"+`
- Candidate base URL: `+"`%[1]s`"+`

## Findings

### Interaction 1: `+"`POST http://baseline.invalid/widgets?dryRun=true`"+`

- Candidate target: `+"`%[1]s/widgets?dryRun=true`"+`
- Latency: 4 ms
- Status transition: `+"`200 -> 404`"+`

#### Request headers

`+"```text"+`
A-Request: first
Content-Type: application/json
Z-Request: last
`+"```"+`

#### Request body

`+"```text"+`
{"name":"widget"}
`+"```"+`

#### Baseline response: `+"`200`"+`

Headers:

`+"```text"+`
Content-Type: application/json
X-Baseline-A: first
X-Baseline-Z: last
`+"```"+`

Body:

`+"```text"+`
{"id":"widget","state":"available"}
`+"```"+`

#### Candidate response: `+"`404`"+`

Headers:

`+"```text"+`
Content-Length: 28
Content-Type: application/problem+json
Date: Mon, 02 Jan 2006 15:04:05 GMT
X-Candidate-A: first
X-Candidate-Z: last
`+"```"+`

Body:

`+"```text"+`
{"error":"widget not found"}
`+"```"+`

### Interaction 2: `+"`GET http://baseline.invalid/missing`"+`

- Candidate target: `+"`%[1]s/missing`"+`
- Latency: 10 ms
- Status transition: `+"`500 -> 200`"+`

#### Request headers

`+"```text"+`
A-Request: first
Z-Request: last
`+"```"+`

#### Request body

_Empty._

#### Baseline response: `+"`500`"+`

Headers:

`+"```text"+`
X-Baseline-A: first
X-Baseline-Z: last
`+"```"+`

Body:

`+"```text"+`
{"error":"baseline unavailable"}
`+"```"+`

#### Candidate response: `+"`200`"+`

Headers:

`+"```text"+`
Content-Length: 36
Content-Type: application/json
Date: Mon, 02 Jan 2006 15:04:05 GMT
X-Candidate-A: first
X-Candidate-Z: last
`+"```"+`

Body:

`+"```text"+`
{"id":"missing","state":"available"}
`+"```"+`
`, fixture.Server.URL),
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
			} else if len(document.Findings) != 1 {
				got.Error = fmt.Sprintf("comparison JSON findings = %d, want 1", len(document.Findings))
			} else {
				finding := document.Findings[0]
				got.JSONStatusTransitions = string(document.Summary.StatusTransitions)
				got.JSONBaselineResponse = string(finding.BaselineResponse)
				got.JSONTransitionBaseline = string(finding.StatusTransition.Baseline)
				got.JSONTransitionCandidate = finding.StatusTransition.Candidate
				got.JSONRequestURL = finding.Request.URL
				got.JSONTargetURL = finding.TargetURL
				got.JSONCandidateStatus = finding.CandidateResponse.Status
				got.JSONCandidateBody = finding.CandidateResponse.Body
				got.JSONLatencyMS = finding.LatencyMS
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

func TestCampaignCompareRejectsMalformedBaselineJUnitBeforeCandidateSideEffects(t *testing.T) {
	defaultConfig := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", defaultConfig)
	writeBaselineHAR(t, filepath.Join("reports", "baseline", "campaign.har.json"), []harRequestFixture{
		{
			Method: "GET",
			URL:    "http://baseline.invalid/probe",
		},
	})
	if err := os.WriteFile(
		filepath.Join("reports", "baseline", "junit.xml"),
		[]byte(`<testsuites><testcase><failure></testsuites>`),
		0o644,
	); err != nil {
		t.Fatalf("write malformed baseline JUnit: %v", err)
	}

	var (
		mu                    sync.Mutex
		candidateRequestCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		candidateRequestCount++
		mu.Unlock()
	}))
	defer server.Close()

	root := cli.NewRootCommand()
	root.SetArgs([]string{"campaign", "compare", "gpt5.6", "--base-url", server.URL})
	err := root.Execute()

	got := malformedJUnitOutcome{}
	if err != nil {
		got.ErrorHasSetupDecodePrefix = strings.HasPrefix(
			err.Error(),
			"baseline replay setup: decode baseline JUnit:",
		)
	}
	mu.Lock()
	got.CandidateRequestCount = candidateRequestCount
	mu.Unlock()
	_, replayStatErr := os.Stat(filepath.Join("reports", "gpt5.6", "replay.har.json"))
	got.ReplayHARExists = !os.IsNotExist(replayStatErr)
	_, jsonStatErr := os.Stat(filepath.Join("reports", "gpt5.6", "comparison.json"))
	got.ComparisonJSONExists = !os.IsNotExist(jsonStatErr)
	_, markdownStatErr := os.Stat(filepath.Join("reports", "gpt5.6", "comparison.md"))
	got.ComparisonMarkdownExists = !os.IsNotExist(markdownStatErr)
	want := malformedJUnitOutcome{
		ErrorHasSetupDecodePrefix: true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaign compare malformed JUnit outcome = %#v, want %#v", got, want)
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

type receivedRequestTarget struct {
	Host       string
	RequestURI string
}

type receivedSemanticRequest struct {
	Method           string
	RequestURI       string
	Host             string
	Accept           string
	ContentType      string
	Authorization    string
	CorrelationID    string
	ContentLength    int64
	TransferEncoding []string
	Connection       string
	AcceptEncoding   string
	Body             string
}

type harRequestFixture struct {
	Method           string
	URL              string
	Headers          []harHeaderFixture
	PostDataText     string
	PostDataEncoding string
	ResponseStatus   int
	ResponseHeaders  []harHeaderFixture
	ResponseBody     string
}

type harHeaderFixture struct {
	Name  string
	Value string
}

type recordedResponse struct {
	Status int
	Marker string
	Body   string
}

type responseLogDocument struct {
	Log responseLog `json:"log"`
}

type responseLog struct {
	Entries []responseLogEntry `json:"entries"`
}

type responseLogEntry struct {
	Response responseLogResponse `json:"response"`
}

type responseLogResponse struct {
	Status  int                 `json:"status"`
	Headers []responseLogHeader `json:"headers"`
	Content responseLogContent  `json:"content"`
}

type responseLogHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type responseLogContent struct {
	Text string `json:"text"`
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
	Findings []struct {
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
	} `json:"findings"`
}

type malformedJUnitOutcome struct {
	ErrorHasSetupDecodePrefix bool
	CandidateRequestCount     int
	ReplayHARExists           bool
	ComparisonJSONExists      bool
	ComparisonMarkdownExists  bool
}

type comparisonReport struct {
	SchemaVersion string              `json:"schema_version"`
	Baseline      comparisonCampaign  `json:"baseline"`
	Candidate     comparisonCandidate `json:"candidate"`
	Summary       comparisonSummary   `json:"summary"`
	Findings      []comparisonFinding `json:"findings"`
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

type comparisonFinding struct {
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

func writeBaselineHAR(t *testing.T, path string, requests []harRequestFixture) {
	t.Helper()

	entries := make([]map[string]any, 0, len(requests))
	for _, request := range requests {
		headers := make([]any, 0, len(request.Headers))
		for _, header := range request.Headers {
			headers = append(headers, map[string]any{
				"name":  header.Name,
				"value": header.Value,
			})
		}
		harRequest := map[string]any{
			"method":  request.Method,
			"url":     request.URL,
			"headers": headers,
		}
		if request.PostDataText != "" {
			harRequest["postData"] = map[string]any{
				"text":     request.PostDataText,
				"encoding": request.PostDataEncoding,
			}
		}
		entry := map[string]any{"request": harRequest}
		if request.ResponseStatus != 0 || len(request.ResponseHeaders) != 0 || request.ResponseBody != "" {
			responseHeaders := make([]any, 0, len(request.ResponseHeaders))
			for _, header := range request.ResponseHeaders {
				responseHeaders = append(responseHeaders, map[string]any{
					"name":  header.Name,
					"value": header.Value,
				})
			}
			entry["response"] = map[string]any{
				"status":  request.ResponseStatus,
				"headers": responseHeaders,
				"content": map[string]any{"text": request.ResponseBody},
			}
		}
		entries = append(entries, entry)
	}
	document := map[string]any{
		"log": map[string]any{
			"version": "1.2",
			"entries": entries,
		},
	}
	contents, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode baseline HAR: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create baseline HAR directory: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write baseline HAR: %v", err)
	}
}

func writeBaselineJUnit(t *testing.T, path string) {
	t.Helper()

	const document = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="schemathesis">
    <testcase name="POST /widgets">
      <failure message="response violates schema">response violates schema</failure>
    </testcase>
    <testcase name="GET /missing">
      <failure message="unexpected server error">unexpected server error</failure>
      <error message="check execution error">check execution error</error>
    </testcase>
  </testsuite>
</testsuites>
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create baseline JUnit directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write baseline JUnit: %v", err)
	}
}

func responseLogHeaderValue(headers []responseLogHeader, name string) string {
	for _, header := range headers {
		if header.Name == name {
			return header.Value
		}
	}

	return ""
}

func serverHost(t *testing.T, rawURL string) string {
	t.Helper()

	host, _, err := net.SplitHostPort(rawURL[len("http://"):])
	if err != nil {
		t.Fatalf("parse server host: %v", err)
	}
	if host == "127.0.0.1" {
		return rawURL[len("http://"):]
	}

	return rawURL[len("http://"):]
}
