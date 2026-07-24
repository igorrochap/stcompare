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
		Output: "replayed 2 baseline interactions\nwrote reports/gpt5.6/replay.har.json\n",
	}

	if got != want {
		t.Fatalf("campaign compare summary = %#v, want %#v", got, want)
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
		entries = append(entries, map[string]any{
			"request": harRequest,
		})
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
