package comparison

import (
	"encoding/json"
	"strings"

	"stcompare/internal/replayoracle"
)

// CustomCheckOracle defines one deterministic replay assertion for a custom
// Schemathesis check.
type CustomCheckOracle = replayoracle.CustomCheckOracle

// StatusReplayOracle identifies candidate statuses that demonstrate the
// corrected behavior.
type StatusReplayOracle = replayoracle.StatusReplayOracle

// JSONResponseReplayOracle identifies a response field and its corrected
// scalar value.
type JSONResponseReplayOracle = replayoracle.JSONResponseReplayOracle

func (p PreconditionPolicy) validateCustomCheckOracles() error {
	return replayoracle.ValidateCustomCheckOracles(p.CustomCheckOracles, "custom_check_oracles")
}

func customCheckOracleFor(
	policy PreconditionPolicy,
	checkName string,
) (CustomCheckOracle, bool) {
	normalizedName := strings.ToLower(strings.TrimSpace(checkName))
	for configuredName, oracle := range policy.CustomCheckOracles {
		if strings.ToLower(strings.TrimSpace(configuredName)) == normalizedName {
			return oracle, true
		}
	}

	return CustomCheckOracle{}, false
}

func classifyCustomCheckProblem(
	oracle CustomCheckOracle,
	interaction reportInteractionEvidence,
) problemClassification {
	if isServerErrorStatus(interaction.CandidateResponse.Status) {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonChangedOutcome)
	}
	if oracle.Status != nil {
		return classifyStatusReplayOracle(*oracle.Status, interaction)
	}
	if oracle.JSON == nil {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayOracleEvidenceMissing)
	}

	return classifyJSONResponseReplayOracle(*oracle.JSON, interaction)
}

func classifyStatusReplayOracle(
	oracle StatusReplayOracle,
	interaction reportInteractionEvidence,
) problemClassification {
	if interaction.StatusTransition.Baseline == nil {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayInteractionMissing)
	}
	baselineStatus := *interaction.StatusTransition.Baseline
	if containsStatus(oracle.AllowedStatuses, baselineStatus) {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayOracleConditionAmbiguous)
	}
	if interaction.CandidateResponse.Status == baselineStatus {
		return problemClassification{
			outcome:       problemOutcomeStillFailing,
			outcomeReason: problemOutcomeReasonReplayOracleStatusRepeated,
		}
	}
	if containsStatus(oracle.AllowedStatuses, interaction.CandidateResponse.Status) {
		return problemClassification{
			outcome:       problemOutcomeFixed,
			outcomeReason: problemOutcomeReasonReplayOracleStatusAllowed,
		}
	}

	return inconclusiveCustomCheckClassification(problemOutcomeReasonChangedOutcome)
}

func classifyJSONResponseReplayOracle(
	oracle JSONResponseReplayOracle,
	interaction reportInteractionEvidence,
) problemClassification {
	if interaction.BaselineResponse == nil {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayInteractionMissing)
	}

	baselineValue, baselineOK := responseJSONField(
		interaction.BaselineResponse.Body,
		oracle.Field,
	)
	if !baselineOK {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayOracleEvidenceMissing)
	}
	candidateValue, candidateOK := responseJSONField(
		interaction.CandidateResponse.Body,
		oracle.Field,
	)
	if !candidateOK {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayOracleEvidenceMissing)
	}
	if !sameJSONValueType(candidateValue, baselineValue) ||
		!sameJSONValueType(candidateValue, oracle.Value) {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayOracleTypeMismatch)
	}
	if jsonValuesEqual(baselineValue, oracle.Value) {
		return inconclusiveCustomCheckClassification(problemOutcomeReasonReplayOracleConditionAmbiguous)
	}
	if jsonValuesEqual(candidateValue, oracle.Value) {
		return problemClassification{
			outcome:       problemOutcomeFixed,
			outcomeReason: problemOutcomeReasonReplayOracleJSONValueAllowed,
		}
	}
	if jsonValuesEqual(candidateValue, baselineValue) {
		return problemClassification{
			outcome:       problemOutcomeStillFailing,
			outcomeReason: problemOutcomeReasonReplayOracleJSONValueRepeated,
		}
	}

	return inconclusiveCustomCheckClassification(problemOutcomeReasonChangedOutcome)
}

func inconclusiveCustomCheckClassification(reason problemOutcomeReason) problemClassification {
	return problemClassification{
		outcome:       problemOutcomeInconclusive,
		outcomeReason: reason,
	}
}

func responseJSONField(body string, field string) (any, bool) {
	var document map[string]any
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		return nil, false
	}
	value, exists := document[field]
	if !exists || value == nil {
		return nil, false
	}

	return value, true
}

func sameJSONValueType(left any, right any) bool {
	return jsonValueType(left) == jsonValueType(right)
}

func jsonValueType(value any) string {
	switch value.(type) {
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return "number"
	default:
		return ""
	}
}

func jsonValuesEqual(left any, right any) bool {
	leftType := jsonValueType(left)
	if leftType == "number" && leftType == jsonValueType(right) {
		return numericJSONValue(left) == numericJSONValue(right)
	}
	return leftType != "" && leftType == jsonValueType(right) && left == right
}

func numericJSONValue(value any) float64 {
	contents, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	var number float64
	if err := json.Unmarshal(contents, &number); err != nil {
		return 0
	}

	return number
}

func containsStatus(statuses []int, candidate int) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}

	return false
}
