package comparison

import "strings"

const schemathesisCaseIDHeader = "X-Schemathesis-TestCaseId"

func correlateBaselineProblems(
	problems []baselineProblem,
	entries []harEntry,
) []baselineProblem {
	correlated := append([]baselineProblem(nil), problems...)
	for problemIndex := range correlated {
		matches := matchingInteractions(correlated[problemIndex].CaseID, entries)
		switch len(matches) {
		case 1:
			interaction := matches[0]
			correlated[problemIndex].Interaction = &interaction
			correlated[problemIndex].CorrelationStatus = correlationStatusCorrelated
		case 0:
			correlated[problemIndex].CorrelationStatus = correlationStatusUncorrelated
		default:
			correlated[problemIndex].CorrelationStatus = correlationStatusAmbiguous
		}
	}

	return correlated
}

// matchingInteractions returns 1-based interaction numbers whose request carries
// the matching Schemathesis case ID header.
func matchingInteractions(caseID string, entries []harEntry) []int {
	if caseID == "" {
		return nil
	}

	matches := make([]int, 0, 1)
	for entryIndex, entry := range entries {
		for _, header := range entry.Request.Headers {
			if !strings.EqualFold(header.Name, schemathesisCaseIDHeader) ||
				header.Value != caseID {
				continue
			}

			matches = append(matches, entryIndex+1)
			break
		}
	}

	return matches
}
