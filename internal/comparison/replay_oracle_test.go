package comparison

import (
	"strings"
	"testing"

	"stcompare/agentreport"
)

func TestCompareRejectsInvalidCustomOracleBeforeReadingBaseline(t *testing.T) {
	_, err := Compare(Input{
		BaselineHARPath: "missing-baseline.har.json",
		PreconditionPolicy: PreconditionPolicy{
			CustomCheckOracles: map[string]CustomCheckOracle{
				"fixture": {},
			},
		},
	}, Dependencies{})
	if err == nil {
		t.Fatal("Compare() error = nil, want invalid-oracle error")
	}
	if !strings.Contains(err.Error(), `baseline replay setup: validate comparison policy: custom_check_oracles["fixture"] must define exactly one of status or json`) {
		t.Fatalf("Compare() error = %q, want actionable custom-oracle validation error", err.Error())
	}
}

func TestCompareRejectsOracleForBuiltInCheckBeforeReadingBaseline(t *testing.T) {
	_, err := Compare(Input{
		BaselineHARPath: "missing-baseline.har.json",
		PreconditionPolicy: PreconditionPolicy{
			CustomCheckOracles: map[string]CustomCheckOracle{
				"not_a_server_error": {
					Status: &StatusReplayOracle{AllowedStatuses: []int{401}},
				},
			},
		},
	}, Dependencies{})
	if err == nil {
		t.Fatal("Compare() error = nil, want built-in oracle collision error")
	}
	if !strings.Contains(err.Error(), `custom_check_oracles["not_a_server_error"] must not define an oracle for a built-in check`) {
		t.Fatalf("Compare() error = %q, want built-in oracle collision error", err.Error())
	}
}

func TestNewReportClassifiesBuiltInCheckBeforeCustomOracle(t *testing.T) {
	document := newCustomOracleReport(
		"not_a_server_error",
		500,
		`{"access":"denied"}`,
		CustomCheckOracle{
			Status: &StatusReplayOracle{AllowedStatuses: []int{403}},
		},
	)

	problem := document.Problems[0]
	if problem.Outcome != problemOutcomeStillFailing {
		t.Fatalf(
			"problem outcome = %q, want still_failing from built-in classifier",
			problem.Outcome,
		)
	}
}

func TestNewReportEvaluatesConfiguredStatusReplayOracle(t *testing.T) {
	tests := []struct {
		name            string
		candidateStatus int
		wantOutcome     problemOutcome
		wantReason      problemOutcomeReason
	}{
		{
			name:            "allowed status fixes the custom check",
			candidateStatus: 403,
			wantOutcome:     problemOutcomeFixed,
			wantReason:      problemOutcomeReasonReplayOracleStatusAllowed,
		},
		{
			name:            "baseline status reproduces the custom check",
			candidateStatus: 200,
			wantOutcome:     problemOutcomeStillFailing,
			wantReason:      problemOutcomeReasonReplayOracleStatusRepeated,
		},
		{
			name:            "unexpected status is inconclusive",
			candidateStatus: 404,
			wantOutcome:     problemOutcomeInconclusive,
			wantReason:      problemOutcomeReasonChangedOutcome,
		},
		{
			name:            "unrelated server error is inconclusive",
			candidateStatus: 500,
			wantOutcome:     problemOutcomeInconclusive,
			wantReason:      problemOutcomeReasonChangedOutcome,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newCustomOracleReport(
				"fixture_access_check",
				test.candidateStatus,
				`{"access":"denied"}`,
				CustomCheckOracle{
					Status: &StatusReplayOracle{AllowedStatuses: []int{401, 403}},
				},
			)

			problem := document.Problems[0]
			if problem.Outcome != test.wantOutcome || problem.OutcomeReason != test.wantReason {
				t.Fatalf(
					"problem outcome = (%q, %q), want (%q, %q)",
					problem.Outcome,
					problem.OutcomeReason,
					test.wantOutcome,
					test.wantReason,
				)
			}
		})
	}
}

func TestNewReportEvaluatesConfiguredJSONReplayOracle(t *testing.T) {
	tests := []struct {
		name          string
		candidateBody string
		wantOutcome   problemOutcome
		wantReason    problemOutcomeReason
	}{
		{
			name:          "expected value fixes the custom check",
			candidateBody: `{"owner":"candidate"}`,
			wantOutcome:   problemOutcomeFixed,
			wantReason:    problemOutcomeReasonReplayOracleJSONValueAllowed,
		},
		{
			name:          "baseline value reproduces the custom check",
			candidateBody: `{"owner":"other"}`,
			wantOutcome:   problemOutcomeStillFailing,
			wantReason:    problemOutcomeReasonReplayOracleJSONValueRepeated,
		},
		{
			name:          "missing field is inconclusive",
			candidateBody: `{"role":"candidate"}`,
			wantOutcome:   problemOutcomeInconclusive,
			wantReason:    problemOutcomeReasonReplayOracleEvidenceMissing,
		},
		{
			name:          "unexpected field type is inconclusive",
			candidateBody: `{"owner":true}`,
			wantOutcome:   problemOutcomeInconclusive,
			wantReason:    problemOutcomeReasonReplayOracleTypeMismatch,
		},
		{
			name:          "unrelated server error is inconclusive",
			candidateBody: `{"owner":"candidate"}`,
			wantOutcome:   problemOutcomeInconclusive,
			wantReason:    problemOutcomeReasonChangedOutcome,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateStatus := 200
			if test.name == "unrelated server error is inconclusive" {
				candidateStatus = 500
			}
			document := newCustomOracleReport(
				"fixture_owner_check",
				candidateStatus,
				test.candidateBody,
				CustomCheckOracle{
					JSON: &JSONResponseReplayOracle{Field: "owner", Value: "candidate"},
				},
			)

			problem := document.Problems[0]
			if problem.Outcome != test.wantOutcome || problem.OutcomeReason != test.wantReason {
				t.Fatalf(
					"problem outcome = (%q, %q), want (%q, %q)",
					problem.Outcome,
					problem.OutcomeReason,
					test.wantOutcome,
					test.wantReason,
				)
			}
		})
	}
}

func TestNewReportKeepsConfiguredOracleInconclusiveWhenReplayEvidenceIsMissing(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "fixture_access_check",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		PreconditionPolicy: PreconditionPolicy{
			CustomCheckOracles: map[string]CustomCheckOracle{
				"fixture_access_check": {
					Status: &StatusReplayOracle{AllowedStatuses: []int{403}},
				},
			},
		},
	})

	problem := document.Problems[0]
	if problem.Outcome != problemOutcomeInconclusive ||
		problem.OutcomeReason != problemOutcomeReasonReplayInteractionMissing {
		t.Fatalf(
			"problem outcome = (%q, %q), want inconclusive replay_interaction_missing",
			problem.Outcome,
			problem.OutcomeReason,
		)
	}
	if document.Summary.BaselineProblems.Evaluable != 1 ||
		document.Summary.BaselineProblems.Inconclusive != 1 {
		t.Fatalf("problem summary = %#v, want one evaluable inconclusive problem", document.Summary.BaselineProblems)
	}
}

func TestNewReportKeepsStatusOracleInconclusiveWhenBaselineStatusIsAllowed(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "fixture_access_check",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-custom",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/1",
					},
				},
			},
		},
		Interactions: []reportInteraction{{
			Baseline: harEntry{
				Request: harRequest{
					Method: "GET",
					URL:    "https://baseline.example.test/widgets/1",
				},
				Response: &harResponse{Status: 403},
			},
			Replay: replayResult{Entry: harEntry{Response: &harResponse{
				Status: 401,
			}}},
		}},
		PreconditionPolicy: PreconditionPolicy{
			CustomCheckOracles: map[string]CustomCheckOracle{
				"fixture_access_check": {
					Status: &StatusReplayOracle{AllowedStatuses: []int{401, 403}},
				},
			},
		},
	})

	problem := document.Problems[0]
	if problem.Outcome != problemOutcomeInconclusive ||
		problem.OutcomeReason != problemOutcomeReasonReplayOracleConditionAmbiguous {
		t.Fatalf(
			"problem outcome = (%q, %q), want inconclusive replay_oracle_condition_ambiguous",
			problem.Outcome,
			problem.OutcomeReason,
		)
	}
}

func TestNewReportLeavesCustomCheckUnevaluableWithoutOracle(t *testing.T) {
	document := newCustomOracleReport(
		"fixture_without_oracle",
		200,
		`{"access":"denied"}`,
		CustomCheckOracle{},
	)

	problem := document.Problems[0]
	if problem.Outcome != problemOutcomeNotEvaluated ||
		problem.OutcomeReason != problemOutcomeReasonUnsupportedCheckCategory {
		t.Fatalf(
			"problem outcome = (%q, %q), want not_evaluated unsupported_check_category",
			problem.Outcome,
			problem.OutcomeReason,
		)
	}
	if document.Summary.BaselineProblems.Fixed != 0 ||
		document.Summary.BaselineProblems.Unevaluable != 1 {
		t.Fatalf("problem summary = %#v, want one unevaluable problem and no fixed problems", document.Summary.BaselineProblems)
	}
}

func TestNewAgentViewIncludesConfiguredCustomCheckStillFailing(t *testing.T) {
	document := newCustomOracleReport(
		"fixture_access_check",
		200,
		`{"access":"denied"}`,
		CustomCheckOracle{
			Status: &StatusReplayOracle{AllowedStatuses: []int{403}},
		},
	)

	view := newAgentView(document)
	if view.Converged || view.Counts.StillFailing != 1 || len(view.Actionable) != 1 {
		t.Fatalf("agent view = %#v, want one actionable still-failing custom check", view)
	}
	if view.Actionable[0].Kind != agentreport.ActionKindStillFailing {
		t.Fatalf("actionable kind = %q, want still_failing", view.Actionable[0].Kind)
	}
}

func newCustomOracleReport(
	checkName string,
	candidateStatus int,
	candidateBody string,
	oracle CustomCheckOracle,
) report {
	interaction := 1
	input := reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         checkName,
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-custom",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/1",
					},
				},
			},
		},
		Interactions: []reportInteraction{{
			Baseline: harEntry{
				Request: harRequest{
					Method: "GET",
					URL:    "https://baseline.example.test/widgets/1",
				},
				Response: &harResponse{
					Status:  200,
					Content: harContent{Text: `{"owner":"other","access":"denied"}`},
				},
			},
			Replay: replayResult{Entry: harEntry{Response: &harResponse{
				Status:  candidateStatus,
				Content: harContent{Text: candidateBody},
			}}},
		}},
	}
	if oracle.Status != nil || oracle.JSON != nil {
		input.PreconditionPolicy.CustomCheckOracles = map[string]CustomCheckOracle{
			checkName: oracle,
		}
	}

	return newReport(input)
}
