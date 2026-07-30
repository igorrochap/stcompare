package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"stcompare/internal/cli"
)

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
	htmlReportPath, pathErr := filepath.Abs(filepath.Join("reports", "gpt5.6", "comparison.html"))
	_, htmlStatErr := os.Stat(filepath.Join("reports", "gpt5.6", "comparison.html"))

	got := configCommandOutcome{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	} else if pathErr != nil {
		got.Error = pathErr.Error()
	} else if htmlStatErr != nil {
		got.Error = htmlStatErr.Error()
	}
	want := configCommandOutcome{
		Output: "replayed 2 baseline interactions\n" +
			"wrote reports/gpt5.6/replay.har.json\n" +
			"wrote reports/gpt5.6/comparison.json\n" +
			"wrote reports/gpt5.6/comparison.md\n" +
			"wrote " + (&url.URL{Scheme: "file", Path: htmlReportPath}).String() + "\n",
	}

	if got != want {
		t.Fatalf("campaign compare summary = %#v, want %#v", got, want)
	}
}
