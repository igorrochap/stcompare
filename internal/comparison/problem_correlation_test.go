package comparison

import (
	"reflect"
	"testing"
)

func TestCorrelateBaselineProblemsUsesCaseIDForRepeatedRequests(t *testing.T) {
	const repeatedURL = "https://baseline.example.test/widgets"
	entries := []harEntry{
		{
			Request: harRequest{
				Method: "POST",
				URL:    repeatedURL,
				Headers: []harHeader{
					{Name: "X-Schemathesis-TestCaseId", Value: "case-1"},
				},
			},
		},
		{
			Request: harRequest{
				Method: "POST",
				URL:    repeatedURL,
				Headers: []harHeader{
					{Name: "X-Schemathesis-TestCaseId", Value: "case-2"},
				},
			},
		},
	}
	problems := []baselineProblem{
		{
			CheckName:      "negative_data_rejection",
			Message:        "Accepted negative data",
			EvidenceSource: "vcr",
			CaseID:         "case-2",
		},
	}

	got := correlateBaselineProblems(problems, entries)

	interaction := 2
	want := []baselineProblem{
		{
			CheckName:         "negative_data_rejection",
			Message:           "Accepted negative data",
			EvidenceSource:    "vcr",
			CaseID:            "case-2",
			CorrelationStatus: correlationStatusCorrelated,
			Interaction:       &interaction,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("correlateBaselineProblems = %#v, want %#v", got, want)
	}
}

func TestCorrelateBaselineProblemsLeavesDuplicateCaseIDAmbiguous(t *testing.T) {
	entries := []harEntry{
		{
			Request: harRequest{
				Headers: []harHeader{
					{Name: "X-Schemathesis-TestCaseId", Value: "case-1"},
				},
			},
		},
		{
			Request: harRequest{
				Headers: []harHeader{
					{Name: "X-Schemathesis-TestCaseId", Value: "case-1"},
				},
			},
		},
	}
	problems := []baselineProblem{
		{
			CheckName:      "negative_data_rejection",
			Message:        "Accepted negative data",
			EvidenceSource: "vcr",
			CaseID:         "case-1",
		},
	}

	got := correlateBaselineProblems(problems, entries)

	want := []baselineProblem{
		{
			CheckName:         "negative_data_rejection",
			Message:           "Accepted negative data",
			EvidenceSource:    "vcr",
			CaseID:            "case-1",
			CorrelationStatus: correlationStatusAmbiguous,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("correlateBaselineProblems ambiguous outcome = %#v, want %#v", got, want)
	}
}
