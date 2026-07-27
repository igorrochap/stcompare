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

	if document.SchemaVersion != "2" {
		t.Fatalf("newReport schema version = %q, want %q", document.SchemaVersion, "2")
	}
}
