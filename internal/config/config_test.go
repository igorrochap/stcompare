package config

import "testing"

func TestConfigValidateRejectsTrimmedDuplicatePreconditionHeuristicNames(t *testing.T) {
	config := Default()
	config.Comparison.PreconditionHeuristics = []PreconditionHeuristic{
		{
			Name:        "generated-widget",
			Method:      "GET",
			PathPattern: `^/widgets/[0-9a-f]+$`,
		},
		{
			Name:        " generated-widget ",
			Method:      "POST",
			PathPattern: `^/widgets$`,
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want duplicate-name error")
	}

	want := "comparison.precondition_heuristics[1].name must be unique"
	if err.Error() != want {
		t.Fatalf("Validate() error = %q, want %q", err.Error(), want)
	}
}
