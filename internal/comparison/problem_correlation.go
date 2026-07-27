package comparison

import "strings"

const schemathesisCaseIDHeader = "X-Schemathesis-TestCaseId"

func correlateBaselineProblems(
	problems []baselineProblem,
	entries []harEntry,
) []baselineProblem {
	correlated := append([]baselineProblem(nil), problems...)
	for problemIndex := range correlated {
		if correlated[problemIndex].CaseID == "" {
			continue
		}

	findInteraction:
		for entryIndex, entry := range entries {
			for _, header := range entry.Request.Headers {
				if !strings.EqualFold(header.Name, schemathesisCaseIDHeader) ||
					header.Value != correlated[problemIndex].CaseID {
					continue
				}

				interaction := entryIndex + 1
				correlated[problemIndex].Interaction = &interaction
				break findInteraction
			}
		}
	}

	return correlated
}
