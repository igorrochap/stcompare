package audit

import (
	"stcompare/benchrecord"
	"testing"
)

func TestSummarizeEfficiencyKeepsKnownTokenSubtotalPartialAndIncludesFailedTiming(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture: Capture{
			Enabled:             true,
			Status:              "complete",
			Complete:            true,
			RecordingOverheadMS: 12,
		},
		Iterations: []Iteration{
			{ID: "iteration-1", Number: 1},
			{ID: "iteration-2", Number: 2},
		},
		Events: []Event{
			{
				Type: "model_turn", IterationID: "iteration-1", Status: "completed",
				DurationMS: 31, EndedAt: "2026-01-01T00:00:01Z", RecordingOverheadMS: 5,
				Tokens: &benchrecord.TokenUsage{Input: 4, Output: 2, Total: 6},
			},
			{
				Type: "model_turn", IterationID: "iteration-1", Status: "failed",
				DurationMS: 7, EndedAt: "2026-01-01T00:00:02Z", RecordingOverheadMS: 3,
			},
			{
				Type: "model_turn", IterationID: "iteration-2", Status: "completed",
				DurationMS: 11, EndedAt: "2026-01-01T00:00:03Z", RecordingOverheadMS: 4,
				Tokens: &benchrecord.TokenUsage{Input: 5, Output: 1, Total: 6},
			},
		},
	}

	summary := SummarizeEfficiency(document)
	if summary.Status != benchrecord.EfficiencyStatusPartial || summary.Turns != 3 ||
		summary.CompletedTurns != 2 || summary.FailedTurns != 1 {
		t.Fatalf("run efficiency = %#v, want complete three-turn evidence", summary)
	}
	if summary.TokenStatus != benchrecord.TokenStatusPartial || summary.KnownTokenTurns != 2 ||
		summary.UnknownTokenTurns != 1 || summary.Tokens == nil {
		t.Fatalf("token evidence = %#v, want partial known subtotal", summary)
	}
	if *summary.Tokens != (benchrecord.TokenUsage{Input: 9, Output: 3, Total: 12}) {
		t.Fatalf("token subtotal = %#v, want input 9/output 3/total 12", summary.Tokens)
	}
	if summary.InferenceMS != 49 || summary.MeasuredInferenceTurns != 3 || summary.RecordingOverheadMS != 12 {
		t.Fatalf("timing evidence = %#v, want inference 49ms and overhead 12ms", summary)
	}

	iteration := summarizeIterationEfficiency(document, "iteration-1")
	if iteration.InferenceMS != 38 || iteration.RecordingOverheadMS != 8 ||
		iteration.TokenStatus != benchrecord.TokenStatusPartial {
		t.Fatalf("iteration efficiency = %#v, want iteration-1 breakdown", iteration)
	}
}

func TestSummarizeEfficiencyKeepsZeroRunOverheadDistinctFromIterationOverhead(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Events: []Event{{
			Type: "model_turn", IterationID: "iteration-1", Status: "completed",
			EndedAt: "2026-01-01T00:00:01Z", RecordingOverheadMS: 4,
		}},
	}

	if summary := SummarizeEfficiency(document); summary.RecordingOverheadMS != 0 {
		t.Fatalf("run overhead = %d, want the recorded zero", summary.RecordingOverheadMS)
	}
	if summary := summarizeIterationEfficiency(document, "iteration-1"); summary.RecordingOverheadMS != 4 {
		t.Fatalf("iteration overhead = %d, want event overhead", summary.RecordingOverheadMS)
	}
}
