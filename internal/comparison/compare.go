package comparison

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Input identifies the baseline evidence, candidate target, and output location
// for one comparison.
type Input struct {
	BaselineCampaign  string
	BaselineHARPath   string
	BaselineJUnitPath string
	CandidateCampaign string
	CandidateBaseURL  string
	OutputDir         string
}

// Dependencies contains replaceable runtime dependencies used by a comparison.
type Dependencies struct {
	Now func() time.Time
}

// Result describes the artifacts produced by a completed comparison.
type Result struct {
	InteractionCount   int
	ReplayLogPath      string
	JSONReportPath     string
	MarkdownReportPath string
}

// Compare replays the baseline interactions and writes the comparison artifacts.
func Compare(input Input, dependencies Dependencies) (Result, error) {
	baselineEntries, err := readHAREntries(input.BaselineHARPath)
	if err != nil {
		return Result{}, fmt.Errorf("baseline replay setup: %w", err)
	}

	problemCount, err := readJUnitProblemCount(input.BaselineJUnitPath)
	if err != nil {
		return Result{}, fmt.Errorf("baseline replay setup: %w", err)
	}
	var problemCountSource *string
	if problemCount != nil {
		problemCountSource = &input.BaselineJUnitPath
	}

	requests := make([]harRequest, 0, len(baselineEntries))
	for _, entry := range baselineEntries {
		requests = append(requests, entry.Request)
	}
	httpRequests, err := newReplayHTTPRequests(input.CandidateBaseURL, requests)
	if err != nil {
		return Result{}, fmt.Errorf("baseline replay setup: %w", err)
	}

	replayResults, err := replayHARRequests(httpRequests, dependencies.Now)
	if err != nil {
		return Result{}, fmt.Errorf("candidate API: %w", err)
	}

	replayEntries := make([]harEntry, 0, len(replayResults))
	for _, result := range replayResults {
		replayEntries = append(replayEntries, result.Entry)
	}
	replayLogPath := filepath.Join(input.OutputDir, "replay.har.json")
	if err := os.MkdirAll(filepath.Dir(replayLogPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("create replay response log directory: %w", err)
	}
	if err := writeReplayResponseLog(replayLogPath, replayEntries); err != nil {
		return Result{}, err
	}

	report := newReport(reportInput{
		BaselineCampaign:           input.BaselineCampaign,
		BaselineProblemCount:       problemCount,
		BaselineProblemCountSource: problemCountSource,
		CandidateCampaign:          input.CandidateCampaign,
		CandidateBaseURL:           input.CandidateBaseURL,
		BaselineEntries:            baselineEntries,
		ReplayResults:              replayResults,
	})
	jsonReportPath := filepath.Join(input.OutputDir, "comparison.json")
	if err := writeJSONReport(jsonReportPath, report); err != nil {
		return Result{}, err
	}
	markdownReportPath := filepath.Join(input.OutputDir, "comparison.md")
	if err := writeMarkdownReport(markdownReportPath, report); err != nil {
		return Result{}, err
	}

	return Result{
		InteractionCount:   len(replayEntries),
		ReplayLogPath:      replayLogPath,
		JSONReportPath:     jsonReportPath,
		MarkdownReportPath: markdownReportPath,
	}, nil
}
