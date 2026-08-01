package comparison

import (
	"reflect"
	"testing"

	"stcompare/agentreport"
)

func TestNewAgentViewProjectsActionableFindings(t *testing.T) {
	stillFailingRef := 2
	document := report{
		SchemaVersion: "11",
		Converged:     false,
		Baseline:      reportCampaign{Campaign: "baseline"},
		Candidate:     reportCandidate{Campaign: "candidate"},
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Fixed:        3,
				StillFailing: 1,
				Inconclusive: 4,
				Uncorrelated: 2,
				Ambiguous:    1,
				Unevaluable:  1,
			},
			Traffic: trafficSummary{Regressed: 1},
		},
		Problems: []baselineProblem{
			{
				CheckCategory: checkCategoryResponseSchemaConformance,
				Message:       "schema mismatch\nwith detail",
				CaseID:        "case-still-failing",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &stillFailingRef,
			},
			{
				Outcome: problemOutcomeInconclusive,
			},
		},
		allInteractions: []reportInteractionEvidence{
			{
				Interaction:       1,
				Request:           reportRequest{Method: "POST", URL: "https://example.test/z"},
				Classification:    interactionClassificationRegressed,
				StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
				CandidateResponse: reportResponse{Status: 500},
			},
			{
				Interaction:      2,
				Request:          reportRequest{Method: "get", URL: "https://example.test/a"},
				StatusTransition: statusTransition{Baseline: intPointer(200), Candidate: 200},
			},
		},
		Findings: []reportInteractionEvidence{
			{
				Interaction:       1,
				Request:           reportRequest{Method: "POST", URL: "https://example.test/z"},
				Classification:    interactionClassificationRegressed,
				StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
				CandidateResponse: reportResponse{Status: 500},
			},
		},
	}

	got := newAgentView(document)
	if got.SchemaVersion != agentreport.SchemaVersion {
		t.Fatalf("agent schema version = %q, want %q", got.SchemaVersion, agentreport.SchemaVersion)
	}
	if !reflect.DeepEqual(got.Counts, (agentreport.Counts{Fixed: 3, StillFailing: 1, Regressed: 1})) {
		t.Fatalf("agent counts = %#v", got.Counts)
	}
	if !reflect.DeepEqual(got.Unverified, (agentreport.Unverified{Inconclusive: 4, Uncorrelated: 2, Ambiguous: 1, Unevaluable: 1})) {
		t.Fatalf("agent unverified = %#v", got.Unverified)
	}
	if len(got.Actionable) != 2 || got.Actionable[0].Kind != "regressed" || got.Actionable[1].Kind != "still_failing" {
		t.Fatalf("agent actionable = %#v", got.Actionable)
	}
	if got.Actionable[1].Message != "schema mismatch with detail" || got.Actionable[1].Operation != "GET /a" {
		t.Fatalf("agent problem projection = %#v", got.Actionable[1])
	}
	if got.Actionable[1].Status.Baseline == nil || *got.Actionable[1].Status.Baseline != 200 || got.Actionable[1].Status.Candidate == nil || *got.Actionable[1].Status.Candidate != 200 {
		t.Fatalf("agent status projection = %#v", got.Actionable[1].Status)
	}
	if got.Actionable[0].Ref != 1 || got.Actionable[1].Ref != 2 {
		t.Fatalf("agent refs = %#v", got.Actionable)
	}
	if !reflect.DeepEqual(got, newAgentView(document)) {
		t.Fatal("agent view is not stable across projections")
	}
}
