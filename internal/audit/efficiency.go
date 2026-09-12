package audit

import (
	"stcompare/benchrecord"
)

// SummarizeEfficiency returns server-reported token subtotals, model-turn
// timing, and capture overhead for the requested audit scope. A missing usage
// field remains unknown and never becomes a zero-valued usage record.
func SummarizeEfficiency(document Artifact) benchrecord.EfficiencySummary {
	return summarizeEfficiency(document.Capture, document.Events, "")
}

func summarizeIterationEfficiency(document Artifact, iterationID string) benchrecord.EfficiencySummary {
	return summarizeEfficiency(document.Capture, document.Events, iterationID)
}

func summarizeEfficiency(
	capture Capture,
	events []Event,
	iterationID string,
) benchrecord.EfficiencySummary {
	recordingOverheadMS := int64(0)
	if iterationID == "" {
		recordingOverheadMS = capture.RecordingOverheadMS
	}
	summary := benchrecord.EfficiencySummary{
		Status:              efficiencyStatus(capture, events, iterationID),
		TokenStatus:         benchrecord.TokenStatusNotReported,
		RecordingOverheadMS: recordingOverheadMS,
	}
	for _, event := range events {
		if !eventMatchesIteration(event, iterationID) {
			continue
		}
		if event.Type == "model_turn" {
			addTurnEfficiency(&summary, event)
		}
		if iterationID != "" {
			summary.RecordingOverheadMS += event.RecordingOverheadMS
		}
	}
	setTokenStatus(&summary)
	return summary
}

func efficiencyStatus(capture Capture, events []Event, iterationID string) benchrecord.EfficiencyStatus {
	turns := 0
	for _, event := range events {
		if event.Type == "model_turn" && eventMatchesIteration(event, iterationID) {
			turns++
			if EventIsIncomplete(event) || event.Status == "" {
				return benchrecord.EfficiencyStatusPartial
			}
		}
	}
	if turns == 0 {
		return benchrecord.EfficiencyStatusNotReported
	}
	if !capture.Enabled || capture.Status != "complete" || !capture.Complete {
		return benchrecord.EfficiencyStatusPartial
	}
	return benchrecord.EfficiencyStatusComplete
}

func addTurnEfficiency(summary *benchrecord.EfficiencySummary, event Event) {
	summary.Turns++
	switch event.Status {
	case "completed":
		summary.CompletedTurns++
	case "failed":
		summary.FailedTurns++
	default:
		summary.IncompleteTurns++
	}
	if event.Tokens == nil {
		summary.UnknownTokenTurns++
	} else {
		summary.KnownTokenTurns++
		if summary.Tokens == nil {
			summary.Tokens = &benchrecord.TokenUsage{}
		}
		summary.Tokens.Input += event.Tokens.Input
		summary.Tokens.Output += event.Tokens.Output
		summary.Tokens.Total += event.Tokens.Total
	}
	if event.EndedAt != "" || event.Status == "completed" || event.Status == "failed" {
		summary.MeasuredInferenceTurns++
		summary.InferenceMS += event.DurationMS
		return
	}
	summary.UnknownInferenceTurns++
}

func setTokenStatus(summary *benchrecord.EfficiencySummary) {
	if summary.Turns == 0 {
		return
	}
	if summary.UnknownTokenTurns == 0 {
		summary.TokenStatus = benchrecord.TokenStatusComplete
		return
	}
	if summary.KnownTokenTurns > 0 {
		summary.TokenStatus = benchrecord.TokenStatusPartial
		return
	}
	summary.TokenStatus = benchrecord.TokenStatusUnknown
}
