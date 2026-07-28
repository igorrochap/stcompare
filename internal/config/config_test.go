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

func TestConfigValidateRejectsMalformedNormalizationRules(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantError string
	}{
		{
			name: "blank body rule name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.BodyFields = []BodyFieldNormalizationRule{
					{Name: " ", FieldName: "id"},
				}
			},
			wantError: "comparison.normalization.body_fields[0].name is required",
		},
		{
			name: "blank body field name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.BodyFields = []BodyFieldNormalizationRule{
					{Name: "generated-id", FieldName: " "},
				}
			},
			wantError: "comparison.normalization.body_fields[0].field_name is required",
		},
		{
			name: "blank header rule name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.Headers = []HeaderNormalizationRule{
					{Name: " ", HeaderName: "date"},
				}
			},
			wantError: "comparison.normalization.headers[0].name is required",
		},
		{
			name: "blank header name",
			mutate: func(config *Config) {
				config.Comparison.Normalization.Headers = []HeaderNormalizationRule{
					{Name: "date-header", HeaderName: " "},
				}
			},
			wantError: "comparison.normalization.headers[0].header_name is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
			test.mutate(&config)

			err := config.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want normalization rule error")
			}
			if err.Error() != test.wantError {
				t.Fatalf("Validate() error = %q, want %q", err.Error(), test.wantError)
			}
		})
	}
}
