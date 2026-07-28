package comparison

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
)

const (
	reportSchemaVersion         = "4"
	baselineProblemsUnavailable = "Baseline Schemathesis problems could not be extracted from structured evidence."
)

type report struct {
	SchemaVersion             string                      `json:"schema_version"`
	Baseline                  reportCampaign              `json:"baseline"`
	Candidate                 reportCandidate             `json:"candidate"`
	ComparisonPolicy          PreconditionPolicy          `json:"comparison"`
	Summary                   reportSummary               `json:"summary"`
	BaselineProblemsAvailable bool                        `json:"baseline_problems_available"`
	BaselineProblemsNote      string                      `json:"baseline_problems_note"`
	Problems                  []baselineProblem           `json:"problems"`
	Findings                  []reportInteractionEvidence `json:"findings"`
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
	InteractionCount  int                    `json:"interaction_count"`
	BaselineProblems  baselineProblemSummary `json:"baseline_problems"`
	Traffic           trafficSummary         `json:"traffic"`
	LatencyMS         reportLatency          `json:"latency_ms"`
	StatusTransitions []statusTransition     `json:"status_transitions"`
}

type baselineProblemSummary struct {
	Total        int `json:"total"`
	Evaluable    int `json:"evaluable"`
	Uncorrelated int `json:"uncorrelated"`
	Fixed        int `json:"fixed"`
	StillFailing int `json:"still_failing"`
	Inconclusive int `json:"inconclusive"`
}

type trafficSummary struct {
	Total            int `json:"total"`
	SuccessUnchanged int `json:"success_unchanged"`
	Changed          int `json:"changed"`
	Regressed        int `json:"regressed"`
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
	Interaction       int                       `json:"interaction"`
	Classification    interactionClassification `json:"classification,omitempty"`
	Request           reportRequest             `json:"request"`
	TargetURL         string                    `json:"target_url"`
	BaselineResponse  *reportResponse           `json:"baseline_response"`
	CandidateResponse reportResponse            `json:"candidate_response"`
	LatencyMS         int                       `json:"latency_ms"`
	StatusTransition  statusTransition          `json:"status_transition"`
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
	PreconditionPolicy         PreconditionPolicy
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

type classifiedReport struct {
	problems         []baselineProblem
	interactions     []reportInteractionEvidence
	baselineProblems baselineProblemSummary
	traffic          trafficSummary
}

type problemClassification struct {
	outcome                      problemOutcome
	outcomeReason                problemOutcomeReason
	matchedPreconditionHeuristic string
}

type interactionClassification string

const (
	interactionClassificationChanged          interactionClassification = "changed"
	interactionClassificationRegressed        interactionClassification = "regressed"
	interactionClassificationSuccessUnchanged interactionClassification = "success_unchanged"
)

func newReport(input reportInput) report {
	interactions := newInteractionEvidence(input.Interactions)
	problemState := input.BaselineProblemEvidence.reportState()
	policy := input.PreconditionPolicy.clone()
	classification := classifyReport(
		problemState.problems,
		interactions,
		policy,
	)
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
		ComparisonPolicy:          policy,
		Summary:                   newReportSummary(input.Interactions, interactions, classification),
		BaselineProblemsAvailable: problemState.available,
		BaselineProblemsNote:      problemState.note,
		Problems:                  classification.problems,
		Findings:                  classification.interactions,
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

func classifyReport(
	problems []baselineProblem,
	interactions []reportInteractionEvidence,
	policy PreconditionPolicy,
) classifiedReport {
	classified := classifiedReport{}
	if problems != nil {
		classified.problems = make([]baselineProblem, len(problems))
		copy(classified.problems, problems)
	}
	if interactions != nil {
		classified.interactions = make([]reportInteractionEvidence, len(interactions))
		copy(classified.interactions, interactions)
	}
	classified.baselineProblems.Total = len(classified.problems)
	classified.traffic.Total = len(interactions)

	for _, problem := range classified.problems {
		if problem.CorrelationStatus == correlationStatusUncorrelated {
			classified.baselineProblems.Uncorrelated++
		}
	}

	for index := range classified.interactions {
		interaction := &classified.interactions[index]
		if isCandidateServerErrorRegression(classified.problems, *interaction) {
			interaction.Classification = interactionClassificationRegressed
			classified.traffic.Regressed++
			continue
		}
		if interaction.StatusTransition.Baseline == nil {
			interaction.Classification = interactionClassificationChanged
			classified.traffic.Changed++
			continue
		}
		if *interaction.StatusTransition.Baseline != interaction.StatusTransition.Candidate {
			interaction.Classification = interactionClassificationChanged
			classified.traffic.Changed++
			continue
		}
		if isServerErrorStatus(interaction.CandidateResponse.Status) {
			interaction.Classification = interactionClassificationChanged
			classified.traffic.Changed++
			continue
		}

		interaction.Classification = interactionClassificationSuccessUnchanged
		classified.traffic.SuccessUnchanged++
	}

	for index := range classified.problems {
		problem := &classified.problems[index]
		if problem.CorrelationStatus != correlationStatusCorrelated {
			continue
		}
		if problem.Interaction == nil || *problem.Interaction < 1 ||
			*problem.Interaction > len(classified.interactions) {
			continue
		}

		interaction := classified.interactions[*problem.Interaction-1]
		classification := classifyProblem(*problem, interaction, policy)
		problem.Outcome = classification.outcome
		problem.OutcomeReason = classification.outcomeReason
		problem.MatchedPreconditionHeuristic =
			classification.matchedPreconditionHeuristic
		if classification.outcome == "" {
			continue
		}
		classified.baselineProblems.Evaluable++
		switch classification.outcome {
		case problemOutcomeStillFailing:
			classified.baselineProblems.StillFailing++
		case problemOutcomeInconclusive:
			classified.baselineProblems.Inconclusive++
		}
	}

	classified.interactions = reportableInteractionFindings(
		classified.problems,
		classified.interactions,
	)

	return classified
}

func classifyProblem(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) problemClassification {
	// Problem outcome precedence:
	// 1. candidate 5xx + server-error check => still_failing
	// 2. matching precondition heuristic => generated_resource_precondition_loss
	// 3. non-heuristic server-error check => inconclusive
	// 4. everything else => no issue-08 outcome
	//
	// Rank 1 cannot overlap rank 2 in valid configuration: precondition
	// heuristics only apply to configured missing-resource statuses, and config
	// validation excludes 5xx from that set.
	if isServerErrorStatus(interaction.CandidateResponse.Status) {
		if isServerErrorCheck(problem.CheckName) {
			return problemClassification{outcome: problemOutcomeStillFailing}
		}

		return problemClassification{}
	}

	heuristic, matched := matchingPreconditionHeuristic(policy, interaction)
	if matched {
		return problemClassification{
			outcome:                      problemOutcomeInconclusive,
			outcomeReason:                problemOutcomeReasonGeneratedResourcePreconditionLoss,
			matchedPreconditionHeuristic: heuristic.Name,
		}
	}

	if isServerErrorCheck(problem.CheckName) {
		return problemClassification{outcome: problemOutcomeInconclusive}
	}

	return problemClassification{}
}

func matchingPreconditionHeuristic(
	policy PreconditionPolicy,
	interaction reportInteractionEvidence,
) (PreconditionHeuristic, bool) {
	if interaction.BaselineResponse == nil ||
		!isSuccessStatus(interaction.BaselineResponse.Status) ||
		!slices.Contains(policy.MissingResourceStatuses, interaction.CandidateResponse.Status) {
		return PreconditionHeuristic{}, false
	}

	requestURL, err := url.Parse(interaction.Request.URL)
	if err != nil {
		return PreconditionHeuristic{}, false
	}
	for _, heuristic := range policy.Heuristics {
		if !strings.EqualFold(interaction.Request.Method, heuristic.Method) {
			continue
		}
		if heuristic.pathPattern.MatchString(requestURL.Path) {
			return heuristic, true
		}
	}

	return PreconditionHeuristic{}, false
}

func isServerErrorCheck(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "not_a_server_error", "server error":
		return true
	}

	return false
}

func isServerErrorStatus(status int) bool {
	return status >= 500 && status <= 599
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status <= 299
}

func isCandidateServerErrorRegression(
	problems []baselineProblem,
	interaction reportInteractionEvidence,
) bool {
	if !isServerErrorStatus(interaction.CandidateResponse.Status) {
		return false
	}
	if interaction.StatusTransition.Baseline != nil &&
		isServerErrorStatus(*interaction.StatusTransition.Baseline) {
		return false
	}
	for _, problem := range problems {
		if problem.Interaction != nil && *problem.Interaction == interaction.Interaction &&
			isServerErrorCheck(problem.CheckName) {
			return false
		}
	}

	return true
}

func reportableInteractionFindings(
	problems []baselineProblem,
	interactions []reportInteractionEvidence,
) []reportInteractionEvidence {
	findings := make([]reportInteractionEvidence, 0, len(interactions))
	for _, interaction := range interactions {
		if interaction.Classification == interactionClassificationSuccessUnchanged {
			continue
		}
		if isPersistentServerErrorExplainedByProblem(problems, interaction) {
			continue
		}
		findings = append(findings, interaction)
	}

	return findings
}

func isPersistentServerErrorExplainedByProblem(
	problems []baselineProblem,
	interaction reportInteractionEvidence,
) bool {
	if interaction.Classification != interactionClassificationChanged ||
		interaction.StatusTransition.Baseline == nil ||
		*interaction.StatusTransition.Baseline != interaction.CandidateResponse.Status ||
		!isServerErrorStatus(interaction.CandidateResponse.Status) {
		return false
	}
	for _, problem := range problems {
		if problem.Interaction != nil && *problem.Interaction == interaction.Interaction &&
			isServerErrorCheck(problem.CheckName) {
			return true
		}
	}

	return false
}

func newReportSummary(
	reportInteractions []reportInteraction,
	evidence []reportInteractionEvidence,
	classification classifiedReport,
) reportSummary {
	return reportSummary{
		InteractionCount:  len(reportInteractions),
		BaselineProblems:  classification.baselineProblems,
		Traffic:           classification.traffic,
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
