package comparison

import (
	"strings"
	"testing"
)

func TestRenderMarkdownOmitsUnavailableDisclosureWhenBaselineProblemsAvailable(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		BaselineProblemsNote:      baselineProblemsUnavailable,
	}

	markdown := renderMarkdown(document)

	if strings.Contains(markdown, "> Baseline Schemathesis problems are unavailable:") {
		t.Fatalf("renderMarkdown included unavailable disclosure:\n%s", markdown)
	}
}

func TestRenderMarkdownIncludesAvailableBaselineProblems(t *testing.T) {
	interaction := 2
	document := report{
		BaselineProblemsAvailable: true,
		Problems: []baselineProblem{
			{
				CheckName:         "status_code_conformance",
				Message:           "Received an undocumented status code: 418",
				EvidenceSource:    "vcr",
				CaseID:            "case-42",
				CorrelationStatus: correlationStatusCorrelated,
				Reproduction: problemReproduction{
					Method: "POST",
					URL:    "https://baseline.example.test/widgets",
					Headers: []harHeader{
						{Name: "Content-Type", Value: "application/json"},
					},
					Body: `{"name":"Ada"}`,
				},
				Interaction: &interaction,
			},
			{
				CheckName:         "API accepted schema-violating request",
				Message:           "Server accepted invalid input.",
				EvidenceSource:    "junit",
				CaseID:            "case-junit",
				CorrelationStatus: correlationStatusUncorrelated,
				Reproduction: problemReproduction{
					Command: "curl https://baseline.example.test/widgets",
				},
			},
		},
	}

	markdown := renderMarkdown(document)

	want := `## Baseline problems

### Problem 1: ` + "`status_code_conformance`" + `

- Message: Received an undocumented status code: 418
- Evidence source: ` + "`vcr`" + `
- Case ID: ` + "`case-42`" + `
- Correlation: interaction 2

#### Reproduction request

- Method: ` + "`POST`" + `
- URL: ` + "`https://baseline.example.test/widgets`" + `

Headers:

` + "```text" + `
Content-Type: application/json
` + "```" + `

Body:

` + "```text" + `
{"name":"Ada"}
` + "```" + `

### Problem 2: ` + "`API accepted schema-violating request`" + `

- Message: Server accepted invalid input.
- Evidence source: ` + "`junit`" + `
- Case ID: ` + "`case-junit`" + `
- Correlation: uncorrelated

#### Reproduction command

` + "```shell" + `
curl https://baseline.example.test/widgets
` + "```" + `
`
	if !strings.Contains(markdown, want) {
		t.Fatalf("renderMarkdown missing baseline problems section:\n%s", markdown)
	}
}

func TestRenderMarkdownMarksAmbiguousBaselineProblemsExplicitly(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Problems: []baselineProblem{
			{
				CheckName:         "status_code_conformance",
				Message:           "Received an undocumented status code: 418",
				EvidenceSource:    "vcr",
				CaseID:            "case-42",
				CorrelationStatus: correlationStatusAmbiguous,
			},
		},
	}

	markdown := renderMarkdown(document)

	if !strings.Contains(markdown, "- Correlation: ambiguous") {
		t.Fatalf("renderMarkdown missing ambiguous correlation status:\n%s", markdown)
	}
}

func TestRenderMarkdownDoesNotPanicOnCorrelatedProblemWithoutInteraction(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Problems: []baselineProblem{
			{
				CheckName:         "status_code_conformance",
				Message:           "Received an undocumented status code: 418",
				EvidenceSource:    evidenceSourceVCR,
				CaseID:            "case-42",
				CorrelationStatus: correlationStatusCorrelated,
			},
		},
	}

	markdown := renderMarkdown(document)

	if !strings.Contains(markdown, "- Correlation: uncorrelated") {
		t.Fatalf("renderMarkdown missing defensive correlation fallback:\n%s", markdown)
	}
}

func TestRenderMarkdownShowsBothAggregateAndExtractedProblemCounts(t *testing.T) {
	aggregateCount := 3
	aggregateSource := "junit.xml"
	extractedCount := 2
	extractedSource := "campaign.vcr.yaml"
	document := report{
		Baseline: reportCampaign{
			ProblemCount:                &aggregateCount,
			ProblemCountSource:          &aggregateSource,
			ExtractedProblemCount:       &extractedCount,
			ExtractedProblemCountSource: &extractedSource,
		},
	}

	markdown := renderMarkdown(document)

	if !strings.Contains(markdown, "- Baseline problems: 3 (source: `junit.xml`)") {
		t.Fatalf("renderMarkdown missing aggregate problem count:\n%s", markdown)
	}
	if !strings.Contains(markdown, "- Extracted baseline problems: 2 (source: `campaign.vcr.yaml`)") {
		t.Fatalf("renderMarkdown missing extracted problem count:\n%s", markdown)
	}
}

func TestRenderMarkdownShowsClassifiedProblemAndTrafficSummaries(t *testing.T) {
	document := report{
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:        2,
				Evaluable:    1,
				Uncorrelated: 1,
				StillFailing: 1,
			},
			Traffic: trafficSummary{
				Total:            3,
				SuccessUnchanged: 1,
				Changed:          1,
				Regressed:        1,
			},
		},
		Problems: []baselineProblem{
			{
				CheckName: "not_a_server_error",
				Outcome:   problemOutcomeStillFailing,
			},
		},
		Findings: []reportInteractionEvidence{
			{
				Interaction:    2,
				Classification: interactionClassificationRegressed,
			},
		},
	}

	markdown := renderMarkdown(document)

	wantLines := []string{
		"- Baseline problem outcomes: total 2, evaluable 1, fixed 0, " +
			"still failing 1, inconclusive 0, uncorrelated 1",
		"- Traffic classifications: total 3, success unchanged 1, changed 1, regressed 1",
		"- Outcome: `still_failing`",
		"- Classification: `regressed`",
	}
	for _, line := range wantLines {
		if !strings.Contains(markdown, line) {
			t.Fatalf("renderMarkdown missing %q:\n%s", line, markdown)
		}
	}
}
