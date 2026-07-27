package comparison

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type replayResult struct {
	Entry     harEntry
	TargetURL string
	LatencyMS int
}

var staleReplayHeaders = []string{
	"Host",
	"Content-Length",
	"Transfer-Encoding",
	"Connection",
	"Accept-Encoding",
}

func newReplayHTTPRequests(baseURL string, requests []harRequest) ([]*http.Request, error) {
	httpRequests := make([]*http.Request, 0, len(requests))
	for index, request := range requests {
		httpRequest, err := newReplayHTTPRequest(baseURL, request)
		if err != nil {
			return nil, fmt.Errorf("create replay request %d: %w", index+1, err)
		}
		httpRequests = append(httpRequests, httpRequest)
	}

	return httpRequests, nil
}

func newReplayHTTPRequest(baseURL string, request harRequest) (*http.Request, error) {
	replayURL, err := candidateReplayURL(baseURL, request.URL)
	if err != nil {
		return nil, fmt.Errorf("prepare replay URL: %w", err)
	}

	var body io.Reader
	if request.PostData.Text != "" {
		body = strings.NewReader(request.PostData.Text)
	}

	httpRequest, err := http.NewRequest(request.Method, replayURL, body)
	if err != nil {
		return nil, err
	}
	for _, header := range request.Headers {
		if staleReplayHeader(header.Name) {
			continue
		}
		httpRequest.Header.Add(header.Name, header.Value)
	}

	return httpRequest, nil
}

func replayHARRequests(requests []*http.Request, now func() time.Time) ([]replayResult, error) {
	client := http.Client{
		Transport: &http.Transport{DisableCompression: true},
	}
	results := make([]replayResult, 0, len(requests))
	for index, request := range requests {
		startedAt := now()
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("send replay request %d: %w", index+1, err)
		}
		entry, err := harEntryFromResponse(response)
		if err != nil {
			return nil, fmt.Errorf("record replay response %d: %w", index+1, err)
		}
		results = append(results, replayResult{
			Entry:     entry,
			TargetURL: request.URL.String(),
			LatencyMS: int(now().Sub(startedAt).Milliseconds()),
		})
	}

	return results, nil
}

func harEntryFromResponse(response *http.Response) (harEntry, error) {
	responseBody, readErr := io.ReadAll(response.Body)
	if err := response.Body.Close(); err != nil {
		return harEntry{}, fmt.Errorf("close body: %w", err)
	}
	if readErr != nil {
		return harEntry{}, fmt.Errorf("read body: %w", readErr)
	}

	return harEntry{
		Response: &harResponse{
			Status:  response.StatusCode,
			Headers: responseHeaders(response.Header),
			Content: harContent{Text: string(responseBody)},
		},
	}, nil
}

func responseHeaders(headers http.Header) []harHeader {
	responseHeaders := make([]harHeader, 0, len(headers))
	for name, values := range headers {
		for _, value := range values {
			responseHeaders = append(responseHeaders, harHeader{
				Name:  name,
				Value: value,
			})
		}
	}

	return sortedHARHeaders(responseHeaders)
}

func staleReplayHeader(name string) bool {
	for _, staleHeader := range staleReplayHeaders {
		if strings.EqualFold(name, staleHeader) {
			return true
		}
	}

	return false
}

func candidateReplayURL(baseURL string, originalURL string) (string, error) {
	candidate, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse candidate base URL: %w", err)
	}
	original, err := url.Parse(originalURL)
	if err != nil {
		return "", fmt.Errorf("parse baseline request URL: %w", err)
	}

	replayed := *candidate
	replayed.Path = original.Path
	replayed.RawPath = original.EscapedPath()
	replayed.RawQuery = original.RawQuery
	replayed.Fragment = ""

	return replayed.String(), nil
}

func writeReplayResponseLog(path string, entries []harEntry) error {
	document := harDocument{
		Log: harLog{
			Version: harVersion,
			Entries: entries,
		},
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode replay response log: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write replay response log: %w", err)
	}

	return nil
}
