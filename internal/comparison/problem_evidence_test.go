package comparison

import (
	"reflect"
	"testing"
)

func TestSelectBaselineProblemEvidenceUsesDeterministicPrecedence(t *testing.T) {
	vcr := baselineProblemEvidence{
		Available: true,
		Problems: []baselineProblem{
			{EvidenceSource: "vcr", CaseID: "vcr-case"},
		},
	}
	ndjson := baselineProblemEvidence{
		Available: true,
		Problems: []baselineProblem{
			{EvidenceSource: "ndjson", CaseID: "ndjson-case"},
		},
	}
	junit := baselineProblemEvidence{
		Available: true,
		Problems: []baselineProblem{
			{EvidenceSource: "junit", CaseID: "junit-case"},
		},
	}
	knownZero := baselineProblemEvidence{
		Available: true,
		Problems:  []baselineProblem{},
	}
	unavailable := baselineProblemEvidence{}

	tests := []struct {
		vcr    baselineProblemEvidence
		ndjson baselineProblemEvidence
		junit  baselineProblemEvidence
	}{
		{vcr: vcr, ndjson: ndjson, junit: junit},
		{ndjson: ndjson, junit: junit},
		{ndjson: knownZero, junit: junit},
		{junit: junit},
		{},
	}
	got := make([]baselineProblemEvidence, 0, len(tests))
	for _, test := range tests {
		got = append(got, selectBaselineProblemEvidence(test.vcr, test.ndjson, test.junit))
	}
	want := []baselineProblemEvidence{
		vcr,
		ndjson,
		knownZero,
		junit,
		unavailable,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectBaselineProblemEvidence results = %#v, want %#v", got, want)
	}
}

func TestReadOptionalProblemEvidenceTreatsIncompleteStructuredEvidenceAsUnavailable(t *testing.T) {
	got, err := readOptionalProblemEvidence(
		"campaign.vcr.yaml",
		func(string) (parsedProblemEvidence, error) {
			return parsedProblemEvidence{
				Complete: false,
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("readOptionalProblemEvidence returned error: %v", err)
	}

	if !reflect.DeepEqual(got, baselineProblemEvidence{}) {
		t.Fatalf("readOptionalProblemEvidence = %#v, want unavailable evidence", got)
	}
}

func TestClassifyCheckStatusRecognizesFailingPassingAndUnknownValues(t *testing.T) {
	tests := []struct {
		status string
		want   checkStatus
	}{
		{status: "FAILURE", want: checkStatusFailing},
		{status: "failure", want: checkStatusFailing},
		{status: "error", want: checkStatusFailing},
		{status: "success", want: checkStatusPassing},
		{status: "skip", want: checkStatusPassing},
		{status: "interrupted", want: checkStatusPassing},
		{status: "nonsense", want: checkStatusUnrecognized},
	}
	for _, test := range tests {
		if got := classifyCheckStatus(test.status); got != test.want {
			t.Fatalf("classifyCheckStatus(%q) = %v, want %v", test.status, got, test.want)
		}
	}
}

func TestNormalizeBaselineProblemsAssignsSharedCheckVocabulary(t *testing.T) {
	problems := []baselineProblem{
		{CheckName: " not_a_server_error "},
		{CheckName: "SERVER ERROR"},
		{CheckName: "response_schema_conformance"},
		{CheckName: "Response violates schema"},
		{CheckName: "negative_data_rejection"},
		{CheckName: "positive_data_acceptance"},
		{CheckName: "unsupported_method"},
	}

	got := normalizedBaselineProblems(problems)

	categories := make([]checkCategory, 0, len(got))
	for _, problem := range got {
		categories = append(categories, problem.CheckCategory)
	}
	want := []checkCategory{
		checkCategoryServerError,
		checkCategoryServerError,
		checkCategoryResponseSchemaConformance,
		checkCategoryResponseSchemaConformance,
		checkCategoryNegativeDataRejection,
		checkCategoryPositiveDataAcceptance,
		checkCategoryUncategorized,
	}
	if !reflect.DeepEqual(categories, want) {
		t.Fatalf("normalizedBaselineProblems categories = %#v, want %#v", categories, want)
	}
	if problems[0].CheckCategory != "" {
		t.Fatalf("normalizedBaselineProblems mutated input category to %q", problems[0].CheckCategory)
	}
}
