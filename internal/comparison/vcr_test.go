package comparison

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadVCRProblemsExpandsFailedChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.vcr.yaml")
	contents := []byte(`
http_interactions:
  - id: case-42
    checks:
      - name: status_code_conformance
        status: FAILURE
        message: "Received an undocumented status code: 418"
      - name: negative_data_rejection
        status: FAILURE
        message: "Accepted negative data"
      - name: not_a_server_error
        status: SUCCESS
        message: null
    request:
      uri: "https://baseline.example.test/widgets"
      method: POST
      headers:
        Content-Type:
          - application/json
      body:
        encoding: utf-8
        base64_string: eyJuYW1lIjoiQWRhIn0=
`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write VCR fixture: %v", err)
	}

	got, err := readVCRProblems(path)
	if err != nil {
		t.Fatalf("readVCRProblems returned error: %v", err)
	}

	want := []baselineProblem{
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
		},
		{
			CheckName:      "negative_data_rejection",
			Message:        "Accepted negative data",
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
		},
	}
	if !got.Complete {
		t.Fatal("readVCRProblems marked complete VCR evidence incomplete")
	}
	if !reflect.DeepEqual(got.Problems, want) {
		t.Fatalf("readVCRProblems problems = %#v, want %#v", got.Problems, want)
	}
}

func TestReadVCRProblemsPreservesPlainRequestBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.vcr.yaml")
	contents := []byte(`
http_interactions:
  - id: case-plain
    checks:
      - name: negative_data_rejection
        status: FAILURE
        message: "Accepted negative data"
    request:
      uri: "https://baseline.example.test/widgets"
      method: POST
      body:
        encoding: utf-8
        string: '{"name":"Ada"}'
`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write VCR fixture: %v", err)
	}

	problems, err := readVCRProblems(path)
	if err != nil {
		t.Fatalf("readVCRProblems returned error: %v", err)
	}

	bodies := make([]string, 0, len(problems.Problems))
	for _, problem := range problems.Problems {
		bodies = append(bodies, problem.Reproduction.Body)
	}
	want := []string{`{"name":"Ada"}`}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("readVCRProblems bodies = %#v, want %#v", bodies, want)
	}
}

func TestReadVCRProblemsMatchesFailureStatusCaseInsensitively(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.vcr.yaml")
	contents := []byte(`
http_interactions:
  - id: case-lower
    checks:
      - name: status_code_conformance
        status: failure
        message: "Received an undocumented status code: 418"
    request:
      uri: "https://baseline.example.test/widgets"
      method: POST
      body:
        string: '{"name":"Ada"}'
`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write VCR fixture: %v", err)
	}

	got, err := readVCRProblems(path)
	if err != nil {
		t.Fatalf("readVCRProblems returned error: %v", err)
	}
	if !got.Complete {
		t.Fatal("readVCRProblems marked lowercase failure status incomplete")
	}
	if len(got.Problems) != 1 {
		t.Fatalf("readVCRProblems problems = %d, want 1", len(got.Problems))
	}
}

func TestReadVCRProblemsMarksUnrecognizedCheckStatusIncomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.vcr.yaml")
	contents := []byte(`
http_interactions:
  - id: case-unknown
    checks:
      - name: status_code_conformance
        status: BROKEN
        message: "Received an undocumented status code: 418"
    request:
      uri: "https://baseline.example.test/widgets"
      method: POST
      body:
        string: '{"name":"Ada"}'
`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write VCR fixture: %v", err)
	}

	got, err := readVCRProblems(path)
	if err != nil {
		t.Fatalf("readVCRProblems returned error: %v", err)
	}
	if got.Complete {
		t.Fatal("readVCRProblems marked unrecognized VCR status complete")
	}
	if len(got.Problems) != 0 {
		t.Fatalf("readVCRProblems problems = %d, want 0", len(got.Problems))
	}
}
