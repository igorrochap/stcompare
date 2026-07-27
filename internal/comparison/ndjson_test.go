package comparison

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadNDJSONProblemsExtractsScenarioFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.ndjson")
	contents := []byte(
		`{"Initialize":{"command":"st run openapi.yaml"}}` + "\n" +
			`{"ScenarioFinished":{"recorder":{"checks":{"case-42":[` +
			`{"name":"status_code_conformance","status":"failure",` +
			`"failure_info":{"failure":{"title":"Undocumented status code","message":""}}},` +
			`{"name":"not_a_server_error","status":"success"}` +
			`]},"interactions":{"case-42":{"request":{` +
			`"method":"POST","uri":"https://baseline.example.test/widgets",` +
			`"headers":{"Content-Type":["application/json"]},` +
			`"body":{"$base64":"eyJuYW1lIjoiQWRhIn0="}` +
			`}}}}}}` + "\n",
	)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write NDJSON fixture: %v", err)
	}

	got, err := readNDJSONProblems(path)
	if err != nil {
		t.Fatalf("readNDJSONProblems returned error: %v", err)
	}

	want := []baselineProblem{
		{
			CheckName:      "status_code_conformance",
			Message:        "Undocumented status code",
			EvidenceSource: "ndjson",
			CaseID:         "case-42",
			Reproduction: problemReproduction{
				Method: "POST",
				URL:    "https://baseline.example.test/widgets",
				Headers: []harHeader{
					{Name: "Content-Type", Value: "application/json"},
				},
				Body: `{"name":"Ada"}`,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readNDJSONProblems = %#v, want %#v", got, want)
	}
}

func TestReadNDJSONProblemsHandlesLargeScenarioEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.ndjson")
	contents := []byte(
		`{"ScenarioFinished":{"padding":"` + strings.Repeat("x", 70*1024) +
			`","recorder":{"checks":{"case-large":[` +
			`{"name":"not_a_server_error","status":"failure",` +
			`"failure_info":{"failure":{"title":"Server error","message":"Received 500"}}}` +
			`]},"interactions":{"case-large":{"request":{` +
			`"method":"GET","uri":"https://baseline.example.test/probe","headers":{}` +
			`}}}}}}` + "\n",
	)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write large NDJSON fixture: %v", err)
	}

	problems, err := readNDJSONProblems(path)

	type problemIdentity struct {
		CaseID    string
		CheckName string
	}
	got := struct {
		Error    string
		Problems []problemIdentity
	}{}
	if err != nil {
		got.Error = err.Error()
	}
	for _, problem := range problems {
		got.Problems = append(got.Problems, problemIdentity{
			CaseID:    problem.CaseID,
			CheckName: problem.CheckName,
		})
	}
	want := struct {
		Error    string
		Problems []problemIdentity
	}{
		Problems: []problemIdentity{
			{
				CaseID:    "case-large",
				CheckName: "not_a_server_error",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readNDJSONProblems large-line outcome = %#v, want %#v", got, want)
	}
}
