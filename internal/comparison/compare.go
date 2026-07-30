package comparison

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// PreconditionPolicy identifies replay responses that may reflect missing
// candidate-side resources rather than fixed baseline problems.
type PreconditionPolicy struct {
	MissingResourceStatuses []int                       `json:"missing_resource_statuses"`
	Heuristics              []PreconditionHeuristic     `json:"precondition_heuristics"`
	Normalization           ResponseNormalizationConfig `json:"normalization"`
}

func (p PreconditionPolicy) clone() PreconditionPolicy {
	clone := PreconditionPolicy{
		MissingResourceStatuses: append([]int(nil), p.MissingResourceStatuses...),
		Heuristics:              append([]PreconditionHeuristic(nil), p.Heuristics...),
		Normalization: ResponseNormalizationConfig{
			Defaults: p.Normalization.Defaults,
			BodyFields: append(
				[]BodyFieldNormalizationRule(nil),
				p.Normalization.BodyFields...,
			),
			Headers: append(
				[]HeaderNormalizationRule(nil),
				p.Normalization.Headers...,
			),
		},
	}

	return clone
}

// PreconditionHeuristic matches one request category in configured order.
type PreconditionHeuristic struct {
	Name        string `json:"name"`
	Method      string `json:"method"`
	PathPattern string `json:"path_pattern"`
	pathPattern *regexp.Regexp
}

func NewPreconditionHeuristic(name, method, pathPattern string) PreconditionHeuristic {
	return PreconditionHeuristic{
		Name:        name,
		Method:      method,
		PathPattern: pathPattern,
		pathPattern: regexp.MustCompile(pathPattern),
	}
}

// Input identifies the baseline evidence, candidate target, and output location
// for one comparison.
type Input struct {
	BaselineCampaign   string
	SchemaPath         string
	BaselineHARPath    string
	BaselineVCRPath    string
	BaselineNDJSONPath string
	BaselineJUnitPath  string
	CandidateCampaign  string
	CandidateBaseURL   string
	OutputDir          string
	PreconditionPolicy PreconditionPolicy
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
	HTMLReportPath     string
}

type preparedComparison struct {
	baselineEntries            []harEntry
	baselineProblemCount       *int
	baselineProblemCountSource *string
	baselineProblemEvidence    baselineProblemEvidence
	schemaValidation           *OpenAPIContract
	replayRequests             []*http.Request
}

// Compare replays the baseline interactions and writes the comparison artifacts.
func Compare(input Input, dependencies Dependencies) (Result, error) {
	prepared, err := prepareComparison(input)
	if err != nil {
		return Result{}, fmt.Errorf("baseline replay setup: %w", err)
	}

	replayResults, err := replayHARRequests(prepared.replayRequests, dependencies.Now)
	if err != nil {
		return Result{}, fmt.Errorf("candidate API: %w", err)
	}

	return persistComparisonArtifacts(input, prepared, replayResults)
}

func prepareComparison(input Input) (preparedComparison, error) {
	baselineEntries, err := readHAREntries(input.BaselineHARPath)
	if err != nil {
		return preparedComparison{}, err
	}

	vcrEvidence, err := readOptionalProblemEvidence(input.BaselineVCRPath, readVCRProblems)
	if err != nil {
		return preparedComparison{}, err
	}
	ndjsonEvidence, err := readOptionalProblemEvidence(
		input.BaselineNDJSONPath,
		readNDJSONProblems,
	)
	if err != nil {
		return preparedComparison{}, err
	}

	problemCount, err := readJUnitProblemCount(input.BaselineJUnitPath)
	if err != nil {
		return preparedComparison{}, err
	}
	var problemCountSource *string
	if problemCount != nil {
		problemCountSource = &input.BaselineJUnitPath
	}

	junitEvidence, err := readOptionalProblemEvidence(input.BaselineJUnitPath, readJUnitProblemEvidence)
	if err != nil {
		return preparedComparison{}, err
	}

	problemEvidence := selectBaselineProblemEvidence(vcrEvidence, ndjsonEvidence, junitEvidence)
	if len(problemEvidence.Problems) != 0 {
		problemEvidence.Problems = correlateBaselineProblems(
			problemEvidence.Problems,
			baselineEntries,
		)
	}
	schemaValidation := LoadOpenAPIContract(input.SchemaPath)

	requests := make([]harRequest, 0, len(baselineEntries))
	for _, entry := range baselineEntries {
		requests = append(requests, entry.Request)
	}
	httpRequests, err := newReplayHTTPRequests(input.CandidateBaseURL, requests)
	if err != nil {
		return preparedComparison{}, err
	}

	return preparedComparison{
		baselineEntries:            baselineEntries,
		baselineProblemCount:       problemCount,
		baselineProblemCountSource: problemCountSource,
		baselineProblemEvidence:    problemEvidence,
		schemaValidation:           schemaValidation,
		replayRequests:             httpRequests,
	}, nil
}

func persistComparisonArtifacts(
	input Input,
	prepared preparedComparison,
	replayResults []replayResult,
) (Result, error) {
	baselineEntries := prepared.baselineEntries
	interactions, err := newReportInteractions(baselineEntries, replayResults)
	if err != nil {
		return Result{}, err
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
		BaselineProblemCount:       prepared.baselineProblemCount,
		BaselineProblemCountSource: prepared.baselineProblemCountSource,
		BaselineProblemEvidence:    prepared.baselineProblemEvidence,
		CandidateCampaign:          input.CandidateCampaign,
		CandidateBaseURL:           input.CandidateBaseURL,
		Interactions:               interactions,
		PreconditionPolicy:         input.PreconditionPolicy,
		SchemaValidation:           prepared.schemaValidation,
	})
	jsonReportPath := filepath.Join(input.OutputDir, "comparison.json")
	if err := writeJSONReport(jsonReportPath, report); err != nil {
		return Result{}, err
	}
	markdownReportPath := filepath.Join(input.OutputDir, "comparison.md")
	if err := writeMarkdownReport(markdownReportPath, report); err != nil {
		return Result{}, err
	}
	htmlReportPath := filepath.Join(input.OutputDir, "comparison.html")
	if err := writeHTMLReport(htmlReportPath, report); err != nil {
		return Result{}, err
	}

	return Result{
		InteractionCount:   len(replayEntries),
		ReplayLogPath:      replayLogPath,
		JSONReportPath:     jsonReportPath,
		MarkdownReportPath: markdownReportPath,
		HTMLReportPath:     htmlReportPath,
	}, nil
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
