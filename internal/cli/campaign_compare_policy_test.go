package cli

import (
	"reflect"
	"testing"

	"stcompare/internal/comparison"
	"stcompare/internal/config"
)

func TestCampaignPreconditionPolicyTrimsHeuristicNameAndMethod(t *testing.T) {
	got := campaignPreconditionPolicy(config.ComparisonConfig{
		MissingResourceStatuses: []int{404},
		PreconditionHeuristics: []config.PreconditionHeuristic{
			{
				Name:        " generated-widget ",
				Method:      " GET ",
				PathPattern: `^/widgets/[0-9a-f]+$`,
			},
		},
	})

	want := comparison.PreconditionPolicy{
		MissingResourceStatuses: []int{404},
		Heuristics: []comparison.PreconditionHeuristic{
			comparison.NewPreconditionHeuristic(
				"generated-widget",
				"GET",
				`^/widgets/[0-9a-f]+$`,
			),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaignPreconditionPolicy() = %#v, want %#v", got, want)
	}
}
