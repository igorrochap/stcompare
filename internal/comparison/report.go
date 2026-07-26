package comparison

import "sort"

const reportSchemaVersion = "1"

type report struct {
	SchemaVersion string          `json:"schema_version"`
	Baseline      reportCampaign  `json:"baseline"`
	Candidate     reportCandidate `json:"candidate"`
	Summary       reportSummary   `json:"summary"`
	Findings      []reportFinding `json:"findings"`
}

type reportCampaign struct {
	Campaign           string  `json:"campaign"`
	ProblemCount       *int    `json:"problem_count"`
	ProblemCountSource *string `json:"problem_count_source"`
}

type reportCandidate struct {
	Campaign string `json:"campaign"`
	BaseURL  string `json:"base_url"`
}

type reportSummary struct {
	InteractionCount  int                     `json:"interaction_count"`
	LatencyMS         reportLatency           `json:"latency_ms"`
	StatusTransitions []statusTransitionCount `json:"status_transitions"`
}

type reportLatency struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
	Average int `json:"average"`
}

type statusTransition struct {
	Baseline  *int `json:"baseline"`
	Candidate int  `json:"candidate"`
}

type statusTransitionCount struct {
	Baseline  int `json:"baseline"`
	Candidate int `json:"candidate"`
	Count     int `json:"count"`
}

type reportFinding struct {
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
	CandidateCampaign          string
	CandidateBaseURL           string
	Interactions               []reportInteraction
}

type reportInteraction struct {
	Baseline harEntry
	Replay   replayResult
}

func newReport(input reportInput) report {
	findings := newReportFindings(input.Interactions)

	return report{
		SchemaVersion: reportSchemaVersion,
		Baseline: reportCampaign{
			Campaign:           input.BaselineCampaign,
			ProblemCount:       input.BaselineProblemCount,
			ProblemCountSource: input.BaselineProblemCountSource,
		},
		Candidate: reportCandidate{
			Campaign: input.CandidateCampaign,
			BaseURL:  input.CandidateBaseURL,
		},
		Summary:  newReportSummary(findings),
		Findings: findings,
	}
}

func newReportSummary(findings []reportFinding) reportSummary {
	return reportSummary{
		InteractionCount:  len(findings),
		LatencyMS:         newReportLatency(findings),
		StatusTransitions: newStatusTransitionCounts(findings),
	}
}

func newReportLatency(findings []reportFinding) reportLatency {
	var latency reportLatency
	latencySum := 0
	for index, finding := range findings {
		latencySum += finding.LatencyMS
		if index == 0 || finding.LatencyMS < latency.Minimum {
			latency.Minimum = finding.LatencyMS
		}
		if index == 0 || finding.LatencyMS > latency.Maximum {
			latency.Maximum = finding.LatencyMS
		}
	}
	if len(findings) != 0 {
		latency.Average = latencySum / len(findings)
	}

	return latency
}

func newStatusTransitionCounts(findings []reportFinding) []statusTransitionCount {
	transitionCounts := make(map[[2]int]int, len(findings))
	for _, finding := range findings {
		if finding.StatusTransition.Baseline != nil {
			transitionCounts[[2]int{
				*finding.StatusTransition.Baseline,
				finding.StatusTransition.Candidate,
			}]++
		}
	}

	transitions := make([]statusTransitionCount, 0, len(transitionCounts))
	for transition, count := range transitionCounts {
		transitions = append(
			transitions,
			statusTransitionCount{
				Baseline:  transition[0],
				Candidate: transition[1],
				Count:     count,
			},
		)
	}
	sort.Slice(transitions, func(i, j int) bool {
		left := transitions[i]
		right := transitions[j]
		if left.Baseline != right.Baseline {
			return left.Baseline < right.Baseline
		}
		return left.Candidate < right.Candidate
	})

	return transitions
}

func newReportFindings(interactions []reportInteraction) []reportFinding {
	findings := make([]reportFinding, 0, len(interactions))
	for index, interaction := range interactions {
		findings = append(
			findings,
			newReportFinding(index+1, interaction.Baseline, interaction.Replay),
		)
	}

	return findings
}

func newReportFinding(
	interaction int,
	baselineEntry harEntry,
	replay replayResult,
) reportFinding {
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

	return reportFinding{
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
