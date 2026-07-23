package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const harVersion = "1.2"

type campaignCompareOptions struct {
	configOverrides configOverrideOptions
}

func newCampaignCompareCommand(rootOpts *rootOptions) *cobra.Command {
	options := campaignCompareOptions{}
	command := &cobra.Command{
		Use:  "compare <candidate>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCampaignCompare(cmd, rootOpts, args[0], options)
		},
	}
	addConfigOverrideFlags(command, &options.configOverrides)

	return command
}

func runCampaignCompare(cmd *cobra.Command, rootOpts *rootOptions, candidateName string, options campaignCompareOptions) error {
	effective, _, err := resolveCampaign(cmd, rootOpts.configPath, candidateName, options.configOverrides)
	if err != nil {
		return err
	}

	requests, err := readHARRequests(filepath.Join(effective.ReportsDir, "baseline", "campaign.har.json"))
	if err != nil {
		return err
	}

	entries, err := replayHARRequests(effective.BaseURL, requests)
	if err != nil {
		return err
	}

	replayLogPath := filepath.Join(effective.ReportsDir, candidateName, "replay.har.json")
	if err := os.MkdirAll(filepath.Dir(replayLogPath), 0o755); err != nil {
		return fmt.Errorf("create replay response log directory: %w", err)
	}
	if err := writeReplayResponseLog(replayLogPath, entries); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "replayed %d baseline interactions\n", len(entries))
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", replayLogPath)

	return nil
}

type harDocument struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version,omitempty"`
	Entries []harEntry `json:"entries"`
}

type harEntry struct {
	Request  harRequest  `json:"request,omitempty"`
	Response harResponse `json:"response,omitempty"`
}

type harRequest struct {
	Method   string      `json:"method"`
	URL      string      `json:"url"`
	Headers  []harHeader `json:"headers"`
	PostData harPostData `json:"postData"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	Text string `json:"text"`
}

type harResponse struct {
	Status  int         `json:"status"`
	Headers []harHeader `json:"headers"`
	Content harContent  `json:"content"`
}

type harContent struct {
	Text string `json:"text"`
}

func readHARRequests(path string) ([]harRequest, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline HAR: %w", err)
	}

	var document harDocument
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("decode baseline HAR: %w", err)
	}

	requests := make([]harRequest, 0, len(document.Log.Entries))
	for _, entry := range document.Log.Entries {
		requests = append(requests, entry.Request)
	}

	return requests, nil
}

func replayHARRequests(baseURL string, requests []harRequest) ([]harEntry, error) {
	client := http.Client{
		Transport: &http.Transport{DisableCompression: true},
	}
	entries := make([]harEntry, 0, len(requests))
	for index, request := range requests {
		httpRequest, err := newReplayHTTPRequest(baseURL, request)
		if err != nil {
			return nil, fmt.Errorf("create replay request %d: %w", index+1, err)
		}
		response, err := client.Do(httpRequest)
		if err != nil {
			return nil, fmt.Errorf("send replay request %d: %w", index+1, err)
		}
		entry, err := harEntryFromResponse(response)
		if err != nil {
			return nil, fmt.Errorf("record replay response %d: %w", index+1, err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
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

func harEntryFromResponse(response *http.Response) (harEntry, error) {
	responseBody, readErr := io.ReadAll(response.Body)
	if err := response.Body.Close(); err != nil {
		return harEntry{}, fmt.Errorf("close body: %w", err)
	}
	if readErr != nil {
		return harEntry{}, fmt.Errorf("read body: %w", readErr)
	}

	return harEntry{
		Response: harResponse{
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

	return responseHeaders
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

func staleReplayHeader(name string) bool {
	staleHeaders := []string{
		"Host",
		"Content-Length",
		"Transfer-Encoding",
		"Connection",
		"Accept-Encoding",
	}
	for _, staleHeader := range staleHeaders {
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
