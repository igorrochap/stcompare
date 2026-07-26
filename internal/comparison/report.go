package comparison

import "sort"

const reportSchemaVersion = "1"

type report struct {
	SchemaVersion string                      `json:"schema_version"`
	Baseline      reportCampaign              `json:"baseline"`
	Candidate     reportCandidate             `json:"candidate"`
	Summary       reportSummary               `json:"summary"`
	Interactions  []reportInteractionEvidence `json:"interactions"`
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
	CandidateCampaign          string
	CandidateBaseURL           string
	Interactions               []reportInteraction
}

type reportInteraction struct {
	Baseline harEntry
	Replay   replayResult
}

func newReport(input reportInput) report {
	interactions := newInteractionEvidence(input.Interactions)

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
		Summary:      newReportSummary(input.Interactions, interactions),
		Interactions: interactions,
	}
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

func newStatusTransitionCounts(evidence []reportInteractionEvidence) []statusTransitionCount {
	transitionCounts := make(map[[2]int]int, len(evidence))
	for _, interaction := range evidence {
		if interaction.StatusTransition.Baseline != nil {
			transitionCounts[[2]int{
				*interaction.StatusTransition.Baseline,
				interaction.StatusTransition.Candidate,
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
