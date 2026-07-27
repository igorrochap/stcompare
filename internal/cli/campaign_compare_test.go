package cli_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

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

func writeBaselineVCR(t *testing.T, path string) {
	t.Helper()

	const document = `
http_interactions:
  - id: case-42
    checks:
      - name: status_code_conformance
        status: FAILURE
        message: "Received an undocumented status code: 418"
    request:
      uri: "http://baseline.invalid/widgets?dryRun=true"
      method: POST
      headers:
        Content-Type:
          - application/json
      body:
        string: "{\"name\":\"widget\"}"
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create baseline VCR directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write baseline VCR: %v", err)
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

	if _, _, err := net.SplitHostPort(rawURL[len("http://"):]); err != nil {
		t.Fatalf("parse server host: %v", err)
	}

	return rawURL[len("http://"):]
}
