package comparison

import (
	"reflect"
	"testing"
)

func TestNewReportPreservesUncorrelatedProblemWhenEvidenceAvailable(t *testing.T) {
	problem := baselineProblem{
		CheckName:         "status_code_conformance",
		Message:           "Received an undocumented status code: 418",
		EvidenceSource:    "vcr",
		CaseID:            "case-missing-from-har",
		CorrelationStatus: correlationStatusUncorrelated,
		Reproduction: problemReproduction{
			Method: "POST",
			URL:    "https://baseline.example.test/widgets",
			Headers: []harHeader{
				{Name: "Content-Type", Value: "application/json"},
			},
			Body: `{"name":"Ada"}`,
		},
	}

	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems:  []baselineProblem{problem},
		},
	})

	got := struct {
		Available bool
		Note      string
		Problems  []baselineProblem
	}{
		Available: document.BaselineProblemsAvailable,
		Note:      document.BaselineProblemsNote,
		Problems:  document.Problems,
	}
	want := struct {
		Available bool
		Note      string
		Problems  []baselineProblem
	}{
		Available: true,
		Problems:  []baselineProblem{problem},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport problem state = %#v, want %#v", got, want)
	}
}

func TestNewReportRepresentsAvailableZeroProblemEvidence(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
	})

	got := struct {
		Available bool
		Problems  []baselineProblem
	}{
		Available: document.BaselineProblemsAvailable,
		Problems:  document.Problems,
	}
	want := struct {
		Available bool
		Problems  []baselineProblem
	}{
		Available: true,
		Problems:  []baselineProblem{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport zero-problem state = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsJUnitProblemCountAndAddsStructuredExtractedCount(t *testing.T) {
	legacyCount := 1
	legacySource := "junit.xml"
	document := newReport(reportInput{
		BaselineProblemCount:       &legacyCount,
		BaselineProblemCountSource: &legacySource,
		BaselineProblemEvidence: baselineProblemEvidence{
			Available:  true,
			SourcePath: "campaign.vcr.yaml",
			Problems: []baselineProblem{
				{EvidenceSource: "vcr", CaseID: "case-1"},
				{EvidenceSource: "vcr", CaseID: "case-2"},
			},
		},
	})

	got := struct {
		ProblemCount                *int
		ProblemCountSource          *string
		ExtractedProblemCount       *int
		ExtractedProblemCountSource *string
	}{
		ProblemCount:                document.Baseline.ProblemCount,
		ProblemCountSource:          document.Baseline.ProblemCountSource,
		ExtractedProblemCount:       document.Baseline.ExtractedProblemCount,
		ExtractedProblemCountSource: document.Baseline.ExtractedProblemCountSource,
	}
	structuredCount := 2
	structuredSource := "campaign.vcr.yaml"
	want := struct {
		ProblemCount                *int
		ProblemCountSource          *string
		ExtractedProblemCount       *int
		ExtractedProblemCountSource *string
	}{
		ProblemCount:                &legacyCount,
		ProblemCountSource:          &legacySource,
		ExtractedProblemCount:       &structuredCount,
		ExtractedProblemCountSource: &structuredSource,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport baseline problem summary = %#v, want %#v", got, want)
	}
}

func TestNewReportUsesCorrelatedProblemSchemaVersion(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					EvidenceSource:    "vcr",
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
	})

	if document.SchemaVersion != "3" {
		t.Fatalf("newReport schema version = %q, want %q", document.SchemaVersion, "3")
	}
}

func TestNewReportClassifiesCorrelatedServerErrorProblemStillFailing(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Response: &harResponse{Status: 500},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{Status: 502},
					},
				},
			},
		},
	})

	got := struct {
		Outcome              problemOutcome
		TrafficClass         interactionClassification
		StillFailingProblems int
	}{
		Outcome:              document.Problems[0].Outcome,
		TrafficClass:         document.Findings[0].Classification,
		StillFailingProblems: document.Summary.BaselineProblems.StillFailing,
	}
	want := struct {
		Outcome              problemOutcome
		TrafficClass         interactionClassification
		StillFailingProblems int
	}{
		Outcome:              problemOutcomeStillFailing,
		TrafficClass:         interactionClassificationChanged,
		StillFailingProblems: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport classification = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsCorrelatedProblemVisibleWhenStatusIsUnchanged(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 500}},
				},
			},
		},
	})

	got := struct {
		ProblemCount    int
		Outcome         problemOutcome
		TrafficFindings int
		ChangedTraffic  int
		HealthyTraffic  int
	}{
		ProblemCount:    len(document.Problems),
		Outcome:         document.Problems[0].Outcome,
		TrafficFindings: len(document.Findings),
		ChangedTraffic:  document.Summary.Traffic.Changed,
		HealthyTraffic:  document.Summary.Traffic.SuccessUnchanged,
	}
	want := struct {
		ProblemCount    int
		Outcome         problemOutcome
		TrafficFindings int
		ChangedTraffic  int
		HealthyTraffic  int
	}{
		ProblemCount:    1,
		Outcome:         problemOutcomeStillFailing,
		TrafficFindings: 0,
		ChangedTraffic:  1,
		HealthyTraffic:  0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport unchanged status problem visibility = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesPersistentServerErrorWithoutProblemAsChanged(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 500}},
				},
			},
		},
	})

	got := struct {
		FindingCount   int
		Classification interactionClassification
		ChangedTraffic int
		HealthyTraffic int
	}{
		FindingCount:   len(document.Findings),
		Classification: document.Findings[0].Classification,
		ChangedTraffic: document.Summary.Traffic.Changed,
		HealthyTraffic: document.Summary.Traffic.SuccessUnchanged,
	}
	want := struct {
		FindingCount   int
		Classification interactionClassification
		ChangedTraffic int
		HealthyTraffic int
	}{
		FindingCount:   1,
		Classification: interactionClassificationChanged,
		ChangedTraffic: 1,
		HealthyTraffic: 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport persistent server error traffic = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCorrelatedServerErrorProblemInconclusive(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 200}},
				},
			},
		},
	})

	got := struct {
		Outcome              problemOutcome
		InconclusiveProblems int
		FixedProblems        int
	}{
		Outcome:              document.Problems[0].Outcome,
		InconclusiveProblems: document.Summary.BaselineProblems.Inconclusive,
		FixedProblems:        document.Summary.BaselineProblems.Fixed,
	}
	want := struct {
		Outcome              problemOutcome
		InconclusiveProblems int
		FixedProblems        int
	}{
		Outcome:              problemOutcomeInconclusive,
		InconclusiveProblems: 1,
		FixedProblems:        0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport inconclusive classification = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCandidateServerErrorWithoutBaselineProblemAsRegression(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 503}},
				},
			},
		},
	})

	got := struct {
		InteractionCount int
		Classification   interactionClassification
		RegressedTraffic int
	}{
		InteractionCount: len(document.Findings),
		Classification:   document.Findings[0].Classification,
		RegressedTraffic: document.Summary.Traffic.Regressed,
	}
	want := struct {
		InteractionCount int
		Classification   interactionClassification
		RegressedTraffic int
	}{
		InteractionCount: 1,
		Classification:   interactionClassificationRegressed,
		RegressedTraffic: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport regression classification = %#v, want %#v", got, want)
	}
}

func TestNewReportCountsAndOmitsHealthyUnchangedInteractionsFromFindings(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 200}},
				},
			},
		},
	})

	got := struct {
		Findings                int
		SuccessUnchangedTraffic int
	}{
		Findings:                len(document.Findings),
		SuccessUnchangedTraffic: document.Summary.Traffic.SuccessUnchanged,
	}
	want := struct {
		Findings                int
		SuccessUnchangedTraffic int
	}{
		Findings:                0,
		SuccessUnchangedTraffic: 1,
	}
	if got != want {
		t.Fatalf("newReport healthy unchanged traffic = %#v, want %#v", got, want)
	}
}

func TestNewReportSeparatesBaselineProblemAndTrafficTotals(t *testing.T) {
	interaction := 1
	nonServerErrorInteraction := 2
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-correlated",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-correlated-schema",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &nonServerErrorInteraction,
				},
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-uncorrelated",
					CorrelationStatus: correlationStatusUncorrelated,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 502}},
				},
			},
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 404}},
				},
			},
		},
	})

	got := struct {
		ProblemTotal      int
		EvaluableProblems int
		Uncorrelated      int
		OutcomeTotal      int
		TrafficTotal      int
		ChangedTraffic    int
	}{
		ProblemTotal:      document.Summary.BaselineProblems.Total,
		EvaluableProblems: document.Summary.BaselineProblems.Evaluable,
		Uncorrelated:      document.Summary.BaselineProblems.Uncorrelated,
		OutcomeTotal: document.Summary.BaselineProblems.Fixed +
			document.Summary.BaselineProblems.StillFailing +
			document.Summary.BaselineProblems.Inconclusive,
		TrafficTotal:   document.Summary.Traffic.Total,
		ChangedTraffic: document.Summary.Traffic.Changed,
	}
	want := struct {
		ProblemTotal      int
		EvaluableProblems int
		Uncorrelated      int
		OutcomeTotal      int
		TrafficTotal      int
		ChangedTraffic    int
	}{
		ProblemTotal:      3,
		EvaluableProblems: 1,
		Uncorrelated:      1,
		OutcomeTotal:      1,
		TrafficTotal:      2,
		ChangedTraffic:    2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport separated summary totals = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCandidateServerErrorRegressionWithDifferentBaselineCheck(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 500}},
				},
			},
		},
	})

	got := struct {
		Classification interactionClassification
		Regressed      int
	}{
		Classification: document.Findings[0].Classification,
		Regressed:      document.Summary.Traffic.Regressed,
	}
	want := struct {
		Classification interactionClassification
		Regressed      int
	}{
		Classification: interactionClassificationRegressed,
		Regressed:      1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport non-corresponding server error regression = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesUnknownBaselineResponseTraffic(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 200}},
				},
			},
			{
				Baseline: harEntry{},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 503}},
				},
			},
		},
	})

	got := struct {
		FirstClassification  interactionClassification
		SecondClassification interactionClassification
		Changed              int
		Regressed            int
	}{
		FirstClassification:  document.Findings[0].Classification,
		SecondClassification: document.Findings[1].Classification,
		Changed:              document.Summary.Traffic.Changed,
		Regressed:            document.Summary.Traffic.Regressed,
	}
	want := struct {
		FirstClassification  interactionClassification
		SecondClassification interactionClassification
		Changed              int
		Regressed            int
	}{
		FirstClassification:  interactionClassificationChanged,
		SecondClassification: interactionClassificationRegressed,
		Changed:              1,
		Regressed:            1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport unknown baseline traffic = %#v, want %#v", got, want)
	}
}
