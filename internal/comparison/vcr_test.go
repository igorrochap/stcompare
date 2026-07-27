package comparison

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadVCRProblemsParsesRealSchemathesisFixture(t *testing.T) {
	got, err := readVCRProblems(filepath.Join("testdata", "schemathesis-real.vcr.yaml"))
	if err != nil {
		t.Fatalf("readVCRProblems returned error: %v", err)
	}

	want := []baselineProblem{
		{
			CheckName:      "not_a_server_error",
			Message:        "Server error",
			EvidenceSource: "vcr",
			CaseID:         "t7i8Oq",
			Reproduction: problemReproduction{
				Method: "TRACE",
				URL:    "http://127.0.0.1:18080/widgets",
				Headers: []harHeader{
					{Name: "Accept", Value: "*/*"},
					{Name: "Accept-Encoding", Value: "gzip, deflate"},
					{Name: "Connection", Value: "keep-alive"},
					{Name: "Content-Length", Value: "12"},
					{Name: "Content-Type", Value: "application/json"},
					{Name: "User-Agent", Value: "schemathesis/4.24.3"},
					{Name: "X-Schemathesis-TestCaseId", Value: "t7i8Oq"},
				},
				Body: "{\"name\": \"\"}",
			},
		},
		{
			CheckName:      "unsupported_method",
			Message:        "Unsupported methods",
			EvidenceSource: "vcr",
			CaseID:         "t7i8Oq",
			Reproduction: problemReproduction{
				Method: "TRACE",
				URL:    "http://127.0.0.1:18080/widgets",
				Headers: []harHeader{
					{Name: "Accept", Value: "*/*"},
					{Name: "Accept-Encoding", Value: "gzip, deflate"},
					{Name: "Connection", Value: "keep-alive"},
					{Name: "Content-Length", Value: "12"},
					{Name: "Content-Type", Value: "application/json"},
					{Name: "User-Agent", Value: "schemathesis/4.24.3"},
					{Name: "X-Schemathesis-TestCaseId", Value: "t7i8Oq"},
				},
				Body: "{\"name\": \"\"}",
			},
		},
		{
			CheckName:      "response_schema_conformance",
			Message:        "Response violates schema",
			EvidenceSource: "vcr",
			CaseID:         "UMioVt",
			Reproduction: problemReproduction{
				Method: "POST",
				URL:    "http://127.0.0.1:18080/widgets",
				Headers: []harHeader{
					{Name: "Accept", Value: "*/*"},
					{Name: "Accept-Encoding", Value: "gzip, deflate"},
					{Name: "Connection", Value: "keep-alive"},
					{Name: "Content-Length", Value: "12"},
					{Name: "Content-Type", Value: "application/json"},
					{Name: "User-Agent", Value: "schemathesis/4.24.3"},
					{Name: "X-Schemathesis-TestCaseId", Value: "UMioVt"},
				},
				Body: "{\"name\": \"\"}",
			},
		},
	}
	if !got.Complete {
		t.Fatal("readVCRProblems marked real Schemathesis fixture incomplete")
	}
	if !reflect.DeepEqual(got.Problems, want) {
		t.Fatalf("readVCRProblems problems = %#v, want %#v", got.Problems, want)
	}
}

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

func TestReadVCRProblemsRoutesRecognizedStatusesThroughAccumulator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.vcr.yaml")
	contents := []byte(`
http_interactions:
  - id: case-problem
    checks:
      - name: status_code_conformance
        status: error
        message: "Received an undocumented status code: 418"
  - id: case-non-problem
    checks:
      - name: not_a_server_error
        status: interrupted
        message: "Run stopped early"
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
		t.Fatal("readVCRProblems marked recognized VCR statuses incomplete")
	}
	if len(got.Problems) != 1 {
		t.Fatalf("readVCRProblems problems = %d, want 1", len(got.Problems))
	}
	if got.Problems[0].CheckName != "status_code_conformance" {
		t.Fatalf("readVCRProblems problem check = %q, want status_code_conformance", got.Problems[0].CheckName)
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
