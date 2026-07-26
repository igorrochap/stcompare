package cli_test

import (
	"fmt"
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
					"baseline campaign: expected exactly one baseline campaign, found %d",
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

type malformedJUnitOutcome struct {
	ErrorHasSetupDecodePrefix bool
	CandidateRequestCount     int
	ReplayHARExists           bool
	ComparisonJSONExists      bool
	ComparisonMarkdownExists  bool
}
