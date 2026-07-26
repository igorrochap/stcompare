package comparison

import (
	"fmt"
	"net/http"
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

type preparedComparison struct {
	baselineEntries            []harEntry
	baselineProblemCount       *int
	baselineProblemCountSource *string
	replayRequests             []*http.Request
}

type comparisonArtifacts struct {
	interactionCount   int
	replayLogPath      string
	jsonReportPath     string
	markdownReportPath string
}

// Compare replays the baseline interactions and writes the comparison artifacts.
func Compare(input Input, dependencies Dependencies) (Result, error) {
	prepared, err := prepareComparison(input)
	if err != nil {
		return Result{}, err
	}

	replayResults, err := replayComparison(prepared, dependencies)
	if err != nil {
		return Result{}, err
	}

	artifacts, err := persistComparisonArtifacts(input, prepared, replayResults)
	if err != nil {
		return Result{}, err
	}

	return newComparisonResult(artifacts), nil
}

func prepareComparison(input Input) (preparedComparison, error) {
	baselineEntries, err := readHAREntries(input.BaselineHARPath)
	if err != nil {
		return preparedComparison{}, fmt.Errorf("baseline replay setup: %w", err)
	}

	problemCount, err := readJUnitProblemCount(input.BaselineJUnitPath)
	if err != nil {
		return preparedComparison{}, fmt.Errorf("baseline replay setup: %w", err)
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
		return preparedComparison{}, fmt.Errorf("baseline replay setup: %w", err)
	}

	return preparedComparison{
		baselineEntries:            baselineEntries,
		baselineProblemCount:       problemCount,
		baselineProblemCountSource: problemCountSource,
		replayRequests:             httpRequests,
	}, nil
}

func replayComparison(
	prepared preparedComparison,
	dependencies Dependencies,
) ([]replayResult, error) {
	replayResults, err := replayHARRequests(prepared.replayRequests, dependencies.Now)
	if err != nil {
		return nil, fmt.Errorf("candidate API: %w", err)
	}

	return replayResults, nil
}

func persistComparisonArtifacts(
	input Input,
	prepared preparedComparison,
	replayResults []replayResult,
) (comparisonArtifacts, error) {
	baselineEntries := prepared.baselineEntries
	interactions, err := newReportInteractions(baselineEntries, replayResults)
	if err != nil {
		return comparisonArtifacts{}, err
	}

	replayEntries := make([]harEntry, 0, len(replayResults))
	for _, result := range replayResults {
		replayEntries = append(replayEntries, result.Entry)
	}
	replayLogPath := filepath.Join(input.OutputDir, "replay.har.json")
	if err := os.MkdirAll(filepath.Dir(replayLogPath), 0o755); err != nil {
		return comparisonArtifacts{}, fmt.Errorf("create replay response log directory: %w", err)
	}
	if err := writeReplayResponseLog(replayLogPath, replayEntries); err != nil {
		return comparisonArtifacts{}, err
	}

	report := newReport(reportInput{
		BaselineCampaign:           input.BaselineCampaign,
		BaselineProblemCount:       prepared.baselineProblemCount,
		BaselineProblemCountSource: prepared.baselineProblemCountSource,
		CandidateCampaign:          input.CandidateCampaign,
		CandidateBaseURL:           input.CandidateBaseURL,
		Interactions:               interactions,
	})
	jsonReportPath := filepath.Join(input.OutputDir, "comparison.json")
	if err := writeJSONReport(jsonReportPath, report); err != nil {
		return comparisonArtifacts{}, err
	}
	markdownReportPath := filepath.Join(input.OutputDir, "comparison.md")
	if err := writeMarkdownReport(markdownReportPath, report); err != nil {
		return comparisonArtifacts{}, err
	}

	return comparisonArtifacts{
		interactionCount:   len(replayEntries),
		replayLogPath:      replayLogPath,
		jsonReportPath:     jsonReportPath,
		markdownReportPath: markdownReportPath,
	}, nil
}

func newComparisonResult(artifacts comparisonArtifacts) Result {
	return Result{
		InteractionCount:   artifacts.interactionCount,
		ReplayLogPath:      artifacts.replayLogPath,
		JSONReportPath:     artifacts.jsonReportPath,
		MarkdownReportPath: artifacts.markdownReportPath,
	}
}

func newReportInteractions(
	baselineEntries []harEntry,
	replayResults []replayResult,
) ([]reportInteraction, error) {
	if len(baselineEntries) != len(replayResults) {
		return nil, fmt.Errorf(
			"pair replay results with baseline entries: got %d replay results for %d baseline entries",
			len(replayResults),
			len(baselineEntries),
		)
	}

	interactions := make([]reportInteraction, 0, len(replayResults))
	for index, replay := range replayResults {
		interactions = append(interactions, reportInteraction{
			Baseline: baselineEntries[index],
			Replay:   replay,
		})
	}

	return interactions, nil
}
