package comparison

import (
	"os"
	"path/filepath"
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
