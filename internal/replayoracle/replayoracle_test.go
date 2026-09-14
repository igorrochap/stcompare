package replayoracle

import (
	"math"
	"strings"
	"testing"
)

func TestValidateCustomCheckOracles(t *testing.T) {
	tests := []struct {
		name      string
		oracles   map[string]CustomCheckOracle
		wantError string
	}{
		{
			name: "empty check name",
			oracles: map[string]CustomCheckOracle{
				" ": {Status: &StatusReplayOracle{AllowedStatuses: []int{401}}},
			},
			wantError: `custom_check_oracles contains an empty check name`,
		},
		{
			name: "duplicate check names",
			oracles: map[string]CustomCheckOracle{
				"Fixture":   {Status: &StatusReplayOracle{AllowedStatuses: []int{401}}},
				" fixture ": {Status: &StatusReplayOracle{AllowedStatuses: []int{403}}},
			},
			wantError: "custom_check_oracles contains duplicate check names",
		},
		{
			name: "built-in check name",
			oracles: map[string]CustomCheckOracle{
				"not_a_server_error": {Status: &StatusReplayOracle{AllowedStatuses: []int{401}}},
			},
			wantError: `custom_check_oracles["not_a_server_error"] must not define an oracle for a built-in check`,
		},
		{
			name: "missing form",
			oracles: map[string]CustomCheckOracle{
				"fixture": {},
			},
			wantError: `custom_check_oracles["fixture"] must define exactly one of status or json`,
		},
		{
			name: "conflicting forms",
			oracles: map[string]CustomCheckOracle{
				"fixture": {
					Status: &StatusReplayOracle{AllowedStatuses: []int{401}},
					JSON:   &JSONResponseReplayOracle{Field: "owner", Value: "candidate"},
				},
			},
			wantError: `custom_check_oracles["fixture"] must define only one of status or json`,
		},
		{
			name: "missing statuses",
			oracles: map[string]CustomCheckOracle{
				"fixture": {Status: &StatusReplayOracle{}},
			},
			wantError: `custom_check_oracles["fixture"].status.allowed_statuses is required`,
		},
		{
			name: "invalid status",
			oracles: map[string]CustomCheckOracle{
				"fixture": {Status: &StatusReplayOracle{AllowedStatuses: []int{600}}},
			},
			wantError: `custom_check_oracles["fixture"].status.allowed_statuses[0] must be a valid HTTP status (100-599)`,
		},
		{
			name: "duplicate status",
			oracles: map[string]CustomCheckOracle{
				"fixture": {Status: &StatusReplayOracle{AllowedStatuses: []int{401, 401}}},
			},
			wantError: `custom_check_oracles["fixture"].status.allowed_statuses must not contain duplicate status 401`,
		},
		{
			name: "missing JSON field",
			oracles: map[string]CustomCheckOracle{
				"fixture": {JSON: &JSONResponseReplayOracle{Value: true}},
			},
			wantError: `custom_check_oracles["fixture"].json.field is required`,
		},
		{
			name: "missing JSON value",
			oracles: map[string]CustomCheckOracle{
				"fixture": {JSON: &JSONResponseReplayOracle{Field: "owner"}},
			},
			wantError: `custom_check_oracles["fixture"].json.value is required`,
		},
		{
			name: "non-scalar JSON value",
			oracles: map[string]CustomCheckOracle{
				"fixture": {JSON: &JSONResponseReplayOracle{
					Field: "owner",
					Value: map[string]any{"name": "candidate"},
				}},
			},
			wantError: `custom_check_oracles["fixture"].json.value must be a JSON scalar`,
		},
		{
			name: "valid oracles",
			oracles: map[string]CustomCheckOracle{
				"fixture_status": {Status: &StatusReplayOracle{AllowedStatuses: []int{401, 403}}},
				"fixture_json":   {JSON: &JSONResponseReplayOracle{Field: "owner", Value: "candidate"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCustomCheckOracles(test.oracles, "custom_check_oracles")
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("ValidateCustomCheckOracles() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf(
					"ValidateCustomCheckOracles() error = %v, want %q",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestIsJSONScalar(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "boolean", value: true, want: true},
		{name: "string", value: "candidate", want: true},
		{name: "integer", value: int64(1), want: true},
		{name: "float32", value: float32(1.5), want: true},
		{name: "float32 NaN", value: float32(math.NaN()), want: false},
		{name: "float64", value: 1.5, want: true},
		{name: "float64 infinity", value: math.Inf(1), want: false},
		{name: "object", value: map[string]any{}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isJSONScalar(test.value); got != test.want {
				t.Fatalf("isJSONScalar(%#v) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}
