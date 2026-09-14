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
		Normalization: comparison.ResponseNormalizationConfig{
			BodyFields: []comparison.BodyFieldNormalizationRule{},
			Headers:    []comparison.HeaderNormalizationRule{},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("campaignPreconditionPolicy() = %#v, want %#v", got, want)
	}
}

func TestCampaignPreconditionPolicyConvertsCustomCheckOracles(t *testing.T) {
	got := campaignPreconditionPolicy(config.ComparisonConfig{
		CustomCheckOracles: map[string]config.CustomCheckOracle{
			" Fixture_Check ": {
				Status: &config.StatusReplayOracle{AllowedStatuses: []int{401, 403}},
			},
		},
	})

	want := comparison.PreconditionPolicy{
		CustomCheckOracles: map[string]comparison.CustomCheckOracle{
			"fixture_check": {
				Status: &comparison.StatusReplayOracle{AllowedStatuses: []int{401, 403}},
			},
		},
	}
	if !reflect.DeepEqual(got.CustomCheckOracles, want.CustomCheckOracles) {
		t.Fatalf(
			"campaignPreconditionPolicy().CustomCheckOracles = %#v, want %#v",
			got.CustomCheckOracles,
			want.CustomCheckOracles,
		)
	}
}
