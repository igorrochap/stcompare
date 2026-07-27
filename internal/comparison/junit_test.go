package comparison

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadJUnitProblemEvidenceParsesRealSchemathesisFixture(t *testing.T) {
	got, err := readJUnitProblemEvidence(filepath.Join("testdata", "schemathesis-real.junit.xml"))
	if err != nil {
		t.Fatalf("readJUnitProblemEvidence returned error: %v", err)
	}

	want := []baselineProblem{
		{
			CheckName:      "Server error",
			Message:        "",
			EvidenceSource: "junit",
			CaseID:         "9tVaJ3",
			Reproduction: problemReproduction{
				Command: `curl -X TRACE -H 'Content-Type: application/json' -d '{"name": ""}' http://127.0.0.1:18080/widgets`,
			},
		},
		{
			CheckName: "Unsupported methods",
			Message: "Unsupported method TRACE returned 501, expected 405 Method Not Allowed\n\n" +
				"    Return 405 for methods not listed in the OpenAPI spec\n\n" +
				"[501] Not Implemented:\n\n" +
				"    `<!DOCTYPE HTML>\n" +
				"    <html lang=\"en\">\n" +
				"        <head>\n" +
				"            <meta charset=\"utf-8\">\n" +
				"            <title>Error response</title>\n" +
				"        </head>\n" +
				"        <body>\n" +
				"            <h1>Error response</h1>\n" +
				"            <p>Error code: 501</p>\n" +
				"            <p>Message: Unsupported method ('TRACE').</p>\n" +
				"            <p>Error code explanation: 501 - Server does not support this operation.</p>\n" +
				"        </body>\n" +
				"    </html>`",
			EvidenceSource: "junit",
			CaseID:         "9tVaJ3",
			Reproduction: problemReproduction{
				Command: `curl -X TRACE -H 'Content-Type: application/json' -d '{"name": ""}' http://127.0.0.1:18080/widgets`,
			},
		},
		{
			CheckName: "Response violates schema",
			Message: "\"name\" is a required property\n\n" +
				"    Validated against the response schema for status code 201.\n\n" +
				"    Schema:\n\n" +
				"        {\n" +
				"            \"required\": [\n" +
				"                \"name\"\n" +
				"            ],\n" +
				"            \"type\": \"object\",\n" +
				"            \"properties\": {\n" +
				"                \"name\": {\n" +
				"                    \"type\": \"string\"\n" +
				"                }\n" +
				"            }\n" +
				"        }\n\n" +
				"    Value:\n\n" +
				"        {\n" +
				"            \"unexpected\": true\n" +
				"        }\n\n" +
				"[201] Created:\n\n" +
				"    `{\"unexpected\": true}`",
			EvidenceSource: "junit",
			CaseID:         "yYpDJG",
			Reproduction: problemReproduction{
				Command: `curl -X POST -H 'Content-Type: application/json' -d '{"name": ""}' http://127.0.0.1:18080/widgets`,
			},
		},
	}
	if !got.Complete {
		t.Fatal("readJUnitProblemEvidence marked real Schemathesis fixture incomplete")
	}
	if !reflect.DeepEqual(got.Problems, want) {
		t.Fatalf("readJUnitProblemEvidence problems = %#v, want %#v", got.Problems, want)
	}
}

func TestReadJUnitProblemCountCountsFailuresAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit.xml")
	contents := []byte(`
<testsuites>
  <testsuite>
    <testcase name="schema">
      <failure message="response violates schema">response violates schema</failure>
    </testcase>
    <testcase name="transport">
      <error message="check execution error">check execution error</error>
    </testcase>
    <testcase name="healthy" />
  </testsuite>
</testsuites>`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write JUnit fixture: %v", err)
	}

	count, err := readJUnitProblemCount(path)
	if err != nil {
		t.Fatalf("readJUnitProblemCount returned error: %v", err)
	}
	if count == nil {
		t.Fatal("readJUnitProblemCount returned nil count")
	}

	want := 2
	if *count != want {
		t.Fatalf("readJUnitProblemCount = %d, want %d", *count, want)
	}
}

func TestReadJUnitProblemEvidenceExtractsFailureEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit.xml")
	contents := []byte(`
<testsuites>
  <testsuite>
    <testcase name="POST /widgets">
      <failure><![CDATA[
1. Test Case ID: case-42

- API accepted schema-violating request

Server accepted invalid input.

Reproduce with:

curl -X POST 'https://baseline.example.test/widgets' \
  -H 'Content-Type: application/json' \
  --data '{"name":"Ada"}'
]]></failure>
    </testcase>
  </testsuite>
</testsuites>`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write JUnit fixture: %v", err)
	}

	got, err := readJUnitProblemEvidence(path)
	if err != nil {
		t.Fatalf("readJUnitProblemEvidence returned error: %v", err)
	}

	want := []baselineProblem{
		{
			CheckName:      "API accepted schema-violating request",
			Message:        "Server accepted invalid input.",
			EvidenceSource: "junit",
			CaseID:         "case-42",
			Reproduction: problemReproduction{
				Command: `curl -X POST 'https://baseline.example.test/widgets' \
  -H 'Content-Type: application/json' \
  --data '{"name":"Ada"}'`,
			},
		},
	}
	if !got.Complete {
		t.Fatal("readJUnitProblemEvidence marked complete JUnit evidence incomplete")
	}
	if !reflect.DeepEqual(got.Problems, want) {
		t.Fatalf("readJUnitProblemEvidence problems = %#v, want %#v", got.Problems, want)
	}
}

func TestReadJUnitProblemEvidenceIgnoresUnstructuredFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit.xml")
	contents := []byte(`
<testsuites>
  <testsuite>
    <testcase name="legacy">
      <failure message="legacy failure">plain legacy failure text</failure>
    </testcase>
  </testsuite>
</testsuites>`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write JUnit fixture: %v", err)
	}

	problems, err := readJUnitProblemEvidence(path)

	got := struct {
		Error    string
		Problems []baselineProblem
		Complete bool
	}{
		Problems: problems.Problems,
		Complete: problems.Complete,
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Problems []baselineProblem
		Complete bool
	}{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readJUnitProblemEvidence unstructured outcome = %#v, want %#v", got, want)
	}
}

func TestReadJUnitProblemEvidenceExpandsNumberedFailureGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit.xml")
	contents := []byte(`
<testsuites>
  <testsuite>
    <testcase>
      <failure><![CDATA[
1. Test Case ID: case-1

- First failed check

First diagnostic.

- Second failed check

Second diagnostic.

Reproduce with:

curl https://baseline.example.test/one

2. Test Case ID: case-2

- Third failed check

Third diagnostic.

Reproduce with:

curl https://baseline.example.test/two
]]></failure>
    </testcase>
  </testsuite>
</testsuites>`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write JUnit fixture: %v", err)
	}

	got, err := readJUnitProblemEvidence(path)
	if err != nil {
		t.Fatalf("readJUnitProblemEvidence returned error: %v", err)
	}

	want := []baselineProblem{
		{
			CheckName:      "First failed check",
			Message:        "First diagnostic.",
			EvidenceSource: "junit",
			CaseID:         "case-1",
			Reproduction: problemReproduction{
				Command: "curl https://baseline.example.test/one",
			},
		},
		{
			CheckName:      "Second failed check",
			Message:        "Second diagnostic.",
			EvidenceSource: "junit",
			CaseID:         "case-1",
			Reproduction: problemReproduction{
				Command: "curl https://baseline.example.test/one",
			},
		},
		{
			CheckName:      "Third failed check",
			Message:        "Third diagnostic.",
			EvidenceSource: "junit",
			CaseID:         "case-2",
			Reproduction: problemReproduction{
				Command: "curl https://baseline.example.test/two",
			},
		},
	}
	if !got.Complete {
		t.Fatal("readJUnitProblemEvidence marked complete JUnit evidence incomplete")
	}
	if !reflect.DeepEqual(got.Problems, want) {
		t.Fatalf("readJUnitProblemEvidence problems = %#v, want %#v", got.Problems, want)
	}
}
