package comparison

import "testing"

func TestNewReportInteractionsRejectsMismatchedInputs(t *testing.T) {
	_, err := newReportInteractions(
		[]harEntry{{}},
		[]replayResult{{}, {}},
	)
	if err == nil {
		t.Fatal("newReportInteractions accepted mismatched inputs")
	}

	want := "pair replay results with baseline entries: got 2 replay results for 1 baseline entries"
	if err.Error() != want {
		t.Fatalf("newReportInteractions error = %q, want %q", err.Error(), want)
	}
}
