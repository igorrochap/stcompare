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
				CheckName:      "status_code_conformance",
				Message:        "Received an undocumented status code: 418",
				EvidenceSource: "vcr",
				CaseID:         "case-42",
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
				CheckName:      "API accepted schema-violating request",
				Message:        "Server accepted invalid input.",
				EvidenceSource: "junit",
				CaseID:         "case-junit",
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
