package audit

import (
	"stcompare/benchrecord"
)

// SummarizeActivity returns model-tool, adapter, and file-edit activity in an
// audit. The status remains partial when the capture cannot establish a
// complete zero, so absent evidence is not mistaken for no activity.
func SummarizeActivity(document Artifact) benchrecord.ActivitySummary {
	if !activityEvidenceReported(document) {
		return notReportedActivity()
	}
	return summarizeEvents(document.Capture, document.Events, document.FileModifications, "")
}

func summarizeIterationActivity(document Artifact, iterationID string) benchrecord.ActivitySummary {
	if !activityEvidenceReported(document) {
		return notReportedActivity()
	}
	return summarizeEvents(document.Capture, document.Events, document.FileModifications, iterationID)
}

func activityEvidenceReported(document Artifact) bool {
	return document.Capture.Enabled && (hasActivityEvents(document.Events) || len(document.FileModifications) > 0)
}

func hasActivityEvents(events []Event) bool {
	for _, event := range events {
		if event.Type == "model_tool_call" || event.Type == "adapter_operation" {
			return true
		}
	}
	return false
}

func notReportedActivity() benchrecord.ActivitySummary {
	return benchrecord.ActivitySummary{Status: benchrecord.ActivityStatusNotReported}
}

func summarizeEvents(
	capture Capture,
	events []Event,
	modifications []FileModification,
	iterationID string,
) benchrecord.ActivitySummary {
	summary := benchrecord.ActivitySummary{Status: activityStatus(capture, events)}
	for _, event := range events {
		if !eventMatchesIteration(event, iterationID) {
			continue
		}
		addEventActivity(&summary, event)
	}
	summary.FileModifications = countFileModifications(modifications, iterationID)
	return summary
}

func eventMatchesIteration(event Event, iterationID string) bool {
	return iterationID == "" || event.IterationID == iterationID
}

func addEventActivity(summary *benchrecord.ActivitySummary, event Event) {
	switch event.Type {
	case "model_tool_call":
		addActivityCount(&summary.ModelToolCalls, event)
		if event.EditAttempt {
			summary.EditAttempts++
		}
	case "adapter_operation":
		addActivityCount(&summary.AdapterOperations, event)
		if event.EditAttempt && event.ModelToolCallID == "" {
			summary.EditAttempts++
		}
	}
}

func countFileModifications(modifications []FileModification, iterationID string) int {
	count := 0
	for _, modification := range modifications {
		if iterationID != "" && modification.IterationID != iterationID {
			continue
		}
		count++
	}
	return count
}

func activityStatus(capture Capture, events []Event) benchrecord.ActivityStatus {
	if !capture.Enabled {
		return benchrecord.ActivityStatusNotReported
	}
	if capture.Status != "complete" || !capture.Complete {
		return benchrecord.ActivityStatusPartial
	}
	for _, event := range events {
		if EventIsIncomplete(event) {
			return benchrecord.ActivityStatusPartial
		}
	}
	return benchrecord.ActivityStatusComplete
}

func addActivityCount(counts *benchrecord.ActivityCounts, event Event) {
	counts.Count++
	switch event.Status {
	case "completed":
		counts.Completed++
		counts.DurationMS += event.DurationMS
	case "failed":
		counts.Failed++
	default:
		counts.Incomplete++
	}
}

// EventIsIncomplete reports whether an audit event lacks a terminal outcome.
func EventIsIncomplete(event Event) bool {
	if event.Type == "model_turn" {
		return event.Status != "completed"
	}
	if event.Type != "model_tool_call" && event.Type != "adapter_operation" {
		return false
	}
	return event.Status != "completed" && event.Status != "failed"
}
