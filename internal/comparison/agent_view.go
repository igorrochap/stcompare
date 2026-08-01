package comparison

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"stcompare/agentreport"
)

func newAgentView(document report) agentreport.View {
	view := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Converged:     document.Converged,
		Candidate:     document.Candidate.Campaign,
		Baseline:      document.Baseline.Campaign,
		Counts: agentreport.Counts{
			Fixed:        document.Summary.BaselineProblems.Fixed,
			StillFailing: document.Summary.BaselineProblems.StillFailing,
			Regressed:    document.Summary.Traffic.Regressed,
		},
		Unverified: agentreport.Unverified{
			Inconclusive: document.Summary.BaselineProblems.Inconclusive,
			Uncorrelated: document.Summary.BaselineProblems.Uncorrelated,
			Ambiguous:    document.Summary.BaselineProblems.Ambiguous,
			Unevaluable:  document.Summary.BaselineProblems.Unevaluable,
		},
		Actionable: make([]agentreport.Actionable, 0),
	}

	for _, problem := range document.Problems {
		if problem.Outcome != problemOutcomeStillFailing || problem.Interaction == nil {
			continue
		}
		if actionable, ok := newProblemActionable(document, problem); ok {
			view.Actionable = append(view.Actionable, actionable)
		}
	}
	for _, interaction := range document.Findings {
		if interaction.Classification != interactionClassificationRegressed {
			continue
		}
		view.Actionable = append(view.Actionable, newRegressionActionable(interaction))
	}

	sort.SliceStable(view.Actionable, func(i, j int) bool {
		left, right := view.Actionable[i], view.Actionable[j]
		if left.Kind != right.Kind {
			return left.Kind == agentreport.ActionKindRegressed
		}
		if left.Operation != right.Operation {
			return left.Operation < right.Operation
		}
		return left.Ref < right.Ref
	})

	return view
}

func newProblemActionable(document report, problem baselineProblem) (agentreport.Actionable, bool) {
	ref := *problem.Interaction
	interaction, ok := interactionByReference(document.allInteractions, ref)
	if !ok {
		return agentreport.Actionable{}, false
	}

	return agentreport.Actionable{
		ID:            stableActionableID(string(problemOutcomeStillFailing), ref, problem.CaseID),
		Kind:          agentreport.ActionKindStillFailing,
		CheckCategory: agentreport.CheckCategory(problem.CheckCategory),
		Operation:     interactionOperation(interaction),
		Status:        interactionStatus(interaction),
		Message:       oneLine(problem.Message),
		Ref:           ref,
	}, true
}

func newRegressionActionable(interaction reportInteractionEvidence) agentreport.Actionable {
	caseID := requestHeaderValue(interaction.Request.Headers, schemathesisCaseIDHeader)
	return agentreport.Actionable{
		ID:   stableActionableID(string(interactionClassificationRegressed), interaction.Interaction, caseID),
		Kind: agentreport.ActionKindRegressed,
		// Regressions are traffic findings, not one of the Schemathesis check
		// categories represented by comparison.check_category.
		CheckCategory: agentreport.CheckCategoryTrafficRegression,
		Operation:     interactionOperation(interaction),
		Status:        interactionStatus(interaction),
		Message: oneLine(fmt.Sprintf(
			"candidate returned status %d; baseline returned %s",
			interaction.CandidateResponse.Status,
			formatStatus(interaction.StatusTransition.Baseline),
		)),
		Ref: interaction.Interaction,
	}
}

func interactionByReference(findings []reportInteractionEvidence, ref int) (reportInteractionEvidence, bool) {
	for _, finding := range findings {
		if finding.Interaction == ref {
			return finding, true
		}
	}
	return reportInteractionEvidence{}, false
}

func interactionOperation(interaction reportInteractionEvidence) string {
	parsed, err := url.Parse(interaction.Request.URL)
	path := interaction.Request.URL
	if err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	return strings.ToUpper(interaction.Request.Method) + " " + path
}

func interactionStatus(interaction reportInteractionEvidence) agentreport.Status {
	return agentreport.Status{
		Baseline:  interaction.StatusTransition.Baseline,
		Candidate: intPointer(interaction.StatusTransition.Candidate),
	}
}

func intPointer(value int) *int { return &value }

func requestHeaderValue(headers []harHeader, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func stableActionableID(kind string, ref int, caseID string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", kind, ref, caseID))))
}

func formatStatus(status *int) string {
	if status == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *status)
}

func oneLine(message string) string {
	return strings.Join(strings.Fields(message), " ")
}
