package comparison

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

const (
	reportSchemaVersion         = "2"
	baselineProblemsUnavailable = "Baseline Schemathesis problems could not be extracted from structured evidence."
)

type report struct {
	SchemaVersion             string                      `json:"schema_version"`
	Baseline                  reportCampaign              `json:"baseline"`
	Candidate                 reportCandidate             `json:"candidate"`
	Summary                   reportSummary               `json:"summary"`
	BaselineProblemsAvailable bool                        `json:"baseline_problems_available"`
	BaselineProblemsNote      string                      `json:"baseline_problems_note"`
	Problems                  []baselineProblem           `json:"problems"`
	Interactions              []reportInteractionEvidence `json:"interactions"`
}

type reportCampaign struct {
	Campaign                    string  `json:"campaign"`
	ProblemCount                *int    `json:"problem_count"`
	ProblemCountSource          *string `json:"problem_count_source"`
	ExtractedProblemCount       *int    `json:"extracted_problem_count"`
	ExtractedProblemCountSource *string `json:"extracted_problem_count_source"`
}

type reportCandidate struct {
	Campaign string `json:"campaign"`
	BaseURL  string `json:"base_url"`
}

type reportSummary struct {
	InteractionCount  int                `json:"interaction_count"`
	LatencyMS         reportLatency      `json:"latency_ms"`
	StatusTransitions []statusTransition `json:"status_transitions"`
}

type reportLatency struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
	Average int `json:"average"`
}

type statusTransition struct {
	Baseline  *int `json:"baseline"`
	Candidate int  `json:"candidate"`
	Count     int  `json:"count,omitempty"`
}

type reportInteractionEvidence struct {
	Interaction       int              `json:"interaction"`
	Request           reportRequest    `json:"request"`
	TargetURL         string           `json:"target_url"`
	BaselineResponse  *reportResponse  `json:"baseline_response"`
	CandidateResponse reportResponse   `json:"candidate_response"`
	LatencyMS         int              `json:"latency_ms"`
	StatusTransition  statusTransition `json:"status_transition"`
}

type reportRequest struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers []harHeader `json:"headers"`
	Body    string      `json:"body"`
}

type reportResponse struct {
	Status  int         `json:"status"`
	Headers []harHeader `json:"headers"`
	Body    string      `json:"body"`
}

type reportInput struct {
	BaselineCampaign           string
	BaselineProblemCount       *int
	BaselineProblemCountSource *string
	BaselineProblemEvidence    baselineProblemEvidence
	CandidateCampaign          string
	CandidateBaseURL           string
	Interactions               []reportInteraction
}

type baselineProblemEvidence struct {
	Available  bool
	SourcePath string
	Problems   []baselineProblem
}

type baselineProblemReportState struct {
	available                   bool
	note                        string
	problems                    []baselineProblem
	extractedProblemCount       *int
	extractedProblemCountSource *string
}

type reportInteraction struct {
	Baseline harEntry
	Replay   replayResult
}

func newReport(input reportInput) report {
	interactions := newInteractionEvidence(input.Interactions)
	problemState := input.BaselineProblemEvidence.reportState()
	problemCount := input.BaselineProblemCount
	problemCountSource := input.BaselineProblemCountSource

	return report{
		SchemaVersion: reportSchemaVersion,
		Baseline: reportCampaign{
			Campaign:                    input.BaselineCampaign,
			ProblemCount:                problemCount,
			ProblemCountSource:          problemCountSource,
			ExtractedProblemCount:       problemState.extractedProblemCount,
			ExtractedProblemCountSource: problemState.extractedProblemCountSource,
		},
		Candidate: reportCandidate{
			Campaign: input.CandidateCampaign,
			BaseURL:  input.CandidateBaseURL,
		},
		Summary:                   newReportSummary(input.Interactions, interactions),
		BaselineProblemsAvailable: problemState.available,
		BaselineProblemsNote:      problemState.note,
		Problems:                  problemState.problems,
		Interactions:              interactions,
	}
}

func (e baselineProblemEvidence) reportState() baselineProblemReportState {
	problems := normalizedBaselineProblems(e.Problems)
	if e.Available && problems == nil {
		problems = []baselineProblem{}
	}

	state := baselineProblemReportState{
		available: e.Available,
		note:      baselineProblemsUnavailable,
		problems:  problems,
	}
	if !e.Available {
		return state
	}

	state.note = ""
	count := len(problems)
	state.extractedProblemCount = &count
	if e.SourcePath != "" {
		source := e.SourcePath
		state.extractedProblemCountSource = &source
	}

	return state
}

func normalizedBaselineProblems(problems []baselineProblem) []baselineProblem {
	if problems == nil {
		return nil
	}

	return append([]baselineProblem(nil), problems...)
}

func newReportSummary(
	reportInteractions []reportInteraction,
	evidence []reportInteractionEvidence,
) reportSummary {
	return reportSummary{
		InteractionCount:  len(reportInteractions),
		LatencyMS:         newReportLatency(evidence),
		StatusTransitions: newStatusTransitionCounts(evidence),
	}
}

func newReportLatency(evidence []reportInteractionEvidence) reportLatency {
	var latency reportLatency
	latencySum := 0
	for index, interaction := range evidence {
		latencySum += interaction.LatencyMS
		if index == 0 || interaction.LatencyMS < latency.Minimum {
			latency.Minimum = interaction.LatencyMS
		}
		if index == 0 || interaction.LatencyMS > latency.Maximum {
			latency.Maximum = interaction.LatencyMS
		}
	}
	if len(evidence) != 0 {
		latency.Average = latencySum / len(evidence)
	}

	return latency
}

func newStatusTransitionCounts(evidence []reportInteractionEvidence) []statusTransition {
	transitionCounts := make(map[[2]int]int, len(evidence))
	for _, interaction := range evidence {
		if interaction.StatusTransition.Baseline != nil {
			transitionCounts[[2]int{
				*interaction.StatusTransition.Baseline,
				interaction.StatusTransition.Candidate,
			}]++
		}
	}

	transitions := make([]statusTransition, 0, len(transitionCounts))
	for transition, count := range transitionCounts {
		baseline := transition[0]
		transitions = append(
			transitions,
			statusTransition{
				Baseline:  &baseline,
				Candidate: transition[1],
				Count:     count,
			},
		)
	}
	sort.Slice(transitions, func(i, j int) bool {
		left := transitions[i]
		right := transitions[j]
		if *left.Baseline != *right.Baseline {
			return *left.Baseline < *right.Baseline
		}
		return left.Candidate < right.Candidate
	})

	return transitions
}

func writeJSONReport(path string, document report) error {
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode comparison JSON report: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write comparison JSON report: %w", err)
	}

	return nil
}

func newInteractionEvidence(interactions []reportInteraction) []reportInteractionEvidence {
	evidence := make([]reportInteractionEvidence, 0, len(interactions))
	for index, interaction := range interactions {
		evidence = append(
			evidence,
			newReportInteractionEvidence(index+1, interaction.Baseline, interaction.Replay),
		)
	}

	return evidence
}

func newReportInteractionEvidence(
	interaction int,
	baselineEntry harEntry,
	replay replayResult,
) reportInteractionEvidence {
	candidateResponse := replay.Entry.Response
	transition := statusTransition{
		Candidate: candidateResponse.Status,
	}
	var baselineResponse *reportResponse
	if baselineEntry.Response != nil {
		baselineStatus := baselineEntry.Response.Status
		transition.Baseline = &baselineStatus
		baselineResponse = &reportResponse{
			Status:  baselineEntry.Response.Status,
			Headers: sortedHARHeaders(baselineEntry.Response.Headers),
			Body:    baselineEntry.Response.Content.Text,
		}
	}

	return reportInteractionEvidence{
		Interaction: interaction,
		Request: reportRequest{
			Method:  baselineEntry.Request.Method,
			URL:     baselineEntry.Request.URL,
			Headers: sortedHARHeaders(baselineEntry.Request.Headers),
			Body:    baselineEntry.Request.PostData.Text,
		},
		TargetURL:        replay.TargetURL,
		BaselineResponse: baselineResponse,
		CandidateResponse: reportResponse{
			Status:  candidateResponse.Status,
			Headers: sortedHARHeaders(candidateResponse.Headers),
			Body:    candidateResponse.Content.Text,
		},
		LatencyMS:        replay.LatencyMS,
		StatusTransition: transition,
	}
}
