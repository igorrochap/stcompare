package comparison

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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

func TestReadJUnitProblemsExtractsFailureEvidence(t *testing.T) {
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

	got, err := readJUnitProblems(path)
	if err != nil {
		t.Fatalf("readJUnitProblems returned error: %v", err)
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readJUnitProblems = %#v, want %#v", got, want)
	}
}

func TestReadJUnitProblemsIgnoresUnstructuredFailure(t *testing.T) {
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

	problems, err := readJUnitProblems(path)

	got := struct {
		Error    string
		Problems []baselineProblem
	}{
		Problems: problems,
	}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Problems []baselineProblem
	}{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readJUnitProblems unstructured outcome = %#v, want %#v", got, want)
	}
}

func TestReadJUnitProblemsExpandsNumberedFailureGroups(t *testing.T) {
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

	got, err := readJUnitProblems(path)
	if err != nil {
		t.Fatalf("readJUnitProblems returned error: %v", err)
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readJUnitProblems = %#v, want %#v", got, want)
	}
}
