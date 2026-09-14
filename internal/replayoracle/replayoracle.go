// Package replayoracle defines shared Schemathesis check rules and replay
// oracle validation.
package replayoracle

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// CustomCheckOracle defines one deterministic replay assertion for a custom
// Schemathesis check.
type CustomCheckOracle struct {
	Status *StatusReplayOracle       `yaml:"status,omitempty" json:"status,omitempty"`
	JSON   *JSONResponseReplayOracle `yaml:"json,omitempty" json:"json,omitempty"`
}

// StatusReplayOracle identifies candidate statuses that demonstrate the
// corrected behavior.
type StatusReplayOracle struct {
	AllowedStatuses []int `yaml:"allowed_statuses" json:"allowed_statuses"`
}

// JSONResponseReplayOracle identifies a response field and its corrected
// scalar value.
type JSONResponseReplayOracle struct {
	Field string `yaml:"field" json:"field"`
	Value any    `yaml:"value" json:"value"`
}

// CheckCategory identifies the built-in classification for a Schemathesis
// check name.
type CheckCategory string

const (
	CheckCategoryServerError                CheckCategory = "server_error"
	CheckCategoryNegativeDataRejection      CheckCategory = "negative_data_rejection"
	CheckCategoryResponseSchemaConformance  CheckCategory = "response_schema_conformance"
	CheckCategoryPositiveDataAcceptance     CheckCategory = "positive_data_acceptance"
	CheckCategoryStatusCodeConformance      CheckCategory = "status_code_conformance"
	CheckCategoryIgnoredAuth                CheckCategory = "ignored_auth"
	CheckCategoryUseAfterFree               CheckCategory = "use_after_free"
	CheckCategoryEnsureResourceAvailability CheckCategory = "ensure_resource_availability"
	CheckCategoryUncategorized              CheckCategory = "uncategorized"
)

var checkCategoriesByName = map[string]CheckCategory{
	"not_a_server_error":           CheckCategoryServerError,
	"server error":                 CheckCategoryServerError,
	"negative_data_rejection":      CheckCategoryNegativeDataRejection,
	"response_schema_conformance":  CheckCategoryResponseSchemaConformance,
	"response violates schema":     CheckCategoryResponseSchemaConformance,
	"positive_data_acceptance":     CheckCategoryPositiveDataAcceptance,
	"status_code_conformance":      CheckCategoryStatusCodeConformance,
	"ignored_auth":                 CheckCategoryIgnoredAuth,
	"use_after_free":               CheckCategoryUseAfterFree,
	"ensure_resource_availability": CheckCategoryEnsureResourceAvailability,
}

// CategorizeCheckName returns the built-in category for a known check name.
func CategorizeCheckName(name string) (CheckCategory, bool) {
	category, ok := checkCategoriesByName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return CheckCategoryUncategorized, false
	}

	return category, true
}

// ValidateCustomCheckOracles validates all configured custom check oracles.
// fieldPath identifies the configuration field in validation errors.
func ValidateCustomCheckOracles(
	oracles map[string]CustomCheckOracle,
	fieldPath string,
) error {
	oracleNames := make(map[string]string, len(oracles))
	names := make([]string, 0, len(oracles))
	for name := range oracles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateCustomCheckOracle(
			name,
			oracles[name],
			fieldPath,
			oracleNames,
		); err != nil {
			return err
		}
	}

	return nil
}

func validateCustomCheckOracle(
	name string,
	oracle CustomCheckOracle,
	fieldPath string,
	oracleNames map[string]string,
) error {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return fmt.Errorf("%s contains an empty check name", fieldPath)
	}
	normalizedName := strings.ToLower(trimmedName)
	if previousName, exists := oracleNames[normalizedName]; exists {
		return fmt.Errorf(
			"%s contains duplicate check names %q and %q",
			fieldPath,
			previousName,
			name,
		)
	}
	oracleNames[normalizedName] = name

	if _, builtIn := CategorizeCheckName(trimmedName); builtIn {
		return fmt.Errorf(
			"%s[%q] must not define an oracle for a built-in check",
			fieldPath,
			name,
		)
	}
	if oracle.Status == nil && oracle.JSON == nil {
		return fmt.Errorf(
			"%s[%q] must define exactly one of status or json",
			fieldPath,
			name,
		)
	}
	if oracle.Status != nil && oracle.JSON != nil {
		return fmt.Errorf(
			"%s[%q] must define only one of status or json",
			fieldPath,
			name,
		)
	}
	if oracle.Status != nil {
		return validateStatusOracle(name, *oracle.Status, fieldPath)
	}
	return validateJSONOracle(name, *oracle.JSON, fieldPath)
}

func validateStatusOracle(
	name string,
	oracle StatusReplayOracle,
	fieldPath string,
) error {
	if len(oracle.AllowedStatuses) == 0 {
		return fmt.Errorf(
			"%s[%q].status.allowed_statuses is required",
			fieldPath,
			name,
		)
	}
	seenStatuses := make(map[int]struct{}, len(oracle.AllowedStatuses))
	for index, status := range oracle.AllowedStatuses {
		if status < 100 || status > 599 {
			return fmt.Errorf(
				"%s[%q].status.allowed_statuses[%d] must be a valid HTTP status (100-599)",
				fieldPath,
				name,
				index,
			)
		}
		if _, exists := seenStatuses[status]; exists {
			return fmt.Errorf(
				"%s[%q].status.allowed_statuses must not contain duplicate status %d",
				fieldPath,
				name,
				status,
			)
		}
		seenStatuses[status] = struct{}{}
	}

	return nil
}

func validateJSONOracle(
	name string,
	oracle JSONResponseReplayOracle,
	fieldPath string,
) error {
	if strings.TrimSpace(oracle.Field) == "" {
		return fmt.Errorf("%s[%q].json.field is required", fieldPath, name)
	}
	if oracle.Value == nil {
		return fmt.Errorf("%s[%q].json.value is required", fieldPath, name)
	}
	if !isJSONScalar(oracle.Value) {
		return fmt.Errorf(
			"%s[%q].json.value must be a JSON scalar",
			fieldPath,
			name,
		)
	}

	return nil
}

func isJSONScalar(value any) bool {
	switch value := value.(type) {
	case bool, string, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
	case float64:
		return !math.IsNaN(value) && !math.IsInf(value, 0)
	default:
		return false
	}
}
