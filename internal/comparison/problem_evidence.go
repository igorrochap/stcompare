package comparison

import (
	"errors"
	"os"
	"strings"
)

type evidenceSource string

const (
	evidenceSourceVCR    evidenceSource = "vcr"
	evidenceSourceNDJSON evidenceSource = "ndjson"
	evidenceSourceJUnit  evidenceSource = "junit"
)

var problemEvidencePrecedence = []evidenceSource{
	evidenceSourceVCR,
	evidenceSourceNDJSON,
	evidenceSourceJUnit,
}

type parsedProblemEvidence struct {
	Problems []baselineProblem
	Complete bool
}

type baselineProblem struct {
	CheckName                    string               `json:"check_name"`
	CheckCategory                checkCategory        `json:"check_category"`
	Message                      string               `json:"message"`
	EvidenceSource               evidenceSource       `json:"evidence_source"`
	CaseID                       string               `json:"case_id"`
	CorrelationStatus            correlationStatus    `json:"correlation_status"`
	Outcome                      problemOutcome       `json:"outcome,omitempty"`
	OutcomeReason                problemOutcomeReason `json:"outcome_reason,omitempty"`
	ExerciseEvidence             []string             `json:"exercise_evidence,omitempty"`
	SchemaValidationErrors       []string             `json:"schema_validation_errors,omitempty"`
	MatchedPreconditionHeuristic string               `json:"matched_precondition_heuristic,omitempty"`
	Reproduction                 problemReproduction  `json:"reproduction"`
	Interaction                  *int                 `json:"interaction"`
}

type checkCategory string

const (
	checkCategoryServerError               checkCategory = "server_error"
	checkCategoryNegativeDataRejection     checkCategory = "negative_data_rejection"
	checkCategoryResponseSchemaConformance checkCategory = "response_schema_conformance"
	checkCategoryPositiveDataAcceptance    checkCategory = "positive_data_acceptance"
	checkCategoryStatusCodeConformance     checkCategory = "status_code_conformance"
	checkCategoryUncategorized             checkCategory = "uncategorized"
)

type problemOutcome string

const (
	problemOutcomeFixed        problemOutcome = "fixed"
	problemOutcomeStillFailing problemOutcome = "still_failing"
	problemOutcomeInconclusive problemOutcome = "inconclusive"
	problemOutcomeNotEvaluated problemOutcome = "not_evaluated"
)

type problemOutcomeReason string

const (
	problemOutcomeReasonGeneratedResourcePreconditionLoss problemOutcomeReason = "generated_resource_precondition_loss"
	problemOutcomeReasonExerciseEvidenceMissing           problemOutcomeReason = "exercise_evidence_missing"
	problemOutcomeReasonAcceptedInvalidData               problemOutcomeReason = "accepted_invalid_data"
	problemOutcomeReasonValidationRejection               problemOutcomeReason = "validation_rejection"
	problemOutcomeReasonAcceptedPositiveData              problemOutcomeReason = "accepted_positive_data"
	problemOutcomeReasonRepeatedRejection                 problemOutcomeReason = "repeated_rejection"
	problemOutcomeReasonStateConflict                     problemOutcomeReason = "state_conflict"
	problemOutcomeReasonChangedOutcome                    problemOutcomeReason = "changed_outcome"
	problemOutcomeReasonRepeatedSchemaViolation           problemOutcomeReason = "repeated_schema_violation"
	problemOutcomeReasonSchemaValidationUnavailable       problemOutcomeReason = "schema_validation_unavailable"
	problemOutcomeReasonSchemaResponseChanged             problemOutcomeReason = "schema_response_changed"
	problemOutcomeReasonSchemaValidationPassed            problemOutcomeReason = "schema_validation_passed"
	problemOutcomeReasonSchemaContractUnavailable         problemOutcomeReason = "schema_contract_unavailable"
	problemOutcomeReasonSchemaContractUnreadable          problemOutcomeReason = "schema_contract_unreadable"
	problemOutcomeReasonSchemaOpenAPIVersionUnsupported   problemOutcomeReason = "schema_openapi_version_unsupported"
	problemOutcomeReasonSchemaReferenceUnresolved         problemOutcomeReason = "schema_reference_unresolved"
	problemOutcomeReasonSchemaOperationNotFound           problemOutcomeReason = "schema_operation_not_found"
	problemOutcomeReasonSchemaOperationAmbiguous          problemOutcomeReason = "schema_operation_ambiguous"
	problemOutcomeReasonSchemaResponseSchemaMissing       problemOutcomeReason = "schema_response_schema_missing"
	problemOutcomeReasonSchemaMediaTypeMissing            problemOutcomeReason = "schema_media_type_missing"
	problemOutcomeReasonSchemaMediaTypeUnsupported        problemOutcomeReason = "schema_media_type_unsupported"
	problemOutcomeReasonSchemaResponseBodyUnavailable     problemOutcomeReason = "schema_response_body_unavailable"
	problemOutcomeReasonSchemaResponseBodyUnparseable     problemOutcomeReason = "schema_response_body_unparseable"
	problemOutcomeReasonStatusCodeDocumented              problemOutcomeReason = "status_code_documented"
	problemOutcomeReasonStatusCodeUndocumented            problemOutcomeReason = "status_code_undocumented"
	problemOutcomeReasonUncorrelatedEvidence              problemOutcomeReason = "uncorrelated_evidence"
	problemOutcomeReasonAmbiguousCorrelation              problemOutcomeReason = "ambiguous_correlation"
	problemOutcomeReasonReplayInteractionMissing          problemOutcomeReason = "replay_interaction_missing"
	problemOutcomeReasonUnsupportedCheckCategory          problemOutcomeReason = "unsupported_check_category"
)

type correlationStatus string

const (
	correlationStatusCorrelated   correlationStatus = "correlated"
	correlationStatusUncorrelated correlationStatus = "uncorrelated"
	correlationStatusAmbiguous    correlationStatus = "ambiguous"
)

type problemReproduction struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers []harHeader `json:"headers"`
	Body    string      `json:"body"`
	Command string      `json:"command,omitempty"`
}

func newStructuredProblemReproduction(
	method string,
	url string,
	headers []harHeader,
	body []byte,
) problemReproduction {
	// Structured evidence carries full HTTP reproduction data; JUnit only carries
	// a replay command and does not use this helper.
	return problemReproduction{
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    string(body),
	}
}

type checkStatus int

const (
	checkStatusPassing checkStatus = iota
	checkStatusFailing
	checkStatusUnrecognized
)

type problemAccumulator struct {
	source     evidenceSource
	problems   []baselineProblem
	incomplete bool
}

func (a *problemAccumulator) observe(status string, build func() baselineProblem) {
	switch classifyCheckStatus(status) {
	case checkStatusFailing:
		a.observeProblems([]baselineProblem{build()})
	case checkStatusUnrecognized:
		a.markIncomplete()
	}
}

func (a *problemAccumulator) observeProblems(problems []baselineProblem) {
	for _, problem := range problems {
		problem.EvidenceSource = a.source
		a.problems = append(a.problems, problem)
	}
}

func (a *problemAccumulator) markIncomplete() {
	a.incomplete = true
}

func (a problemAccumulator) evidence() parsedProblemEvidence {
	return parsedProblemEvidence{
		Problems: a.problems,
		Complete: !a.incomplete,
	}
}

func readOptionalProblemEvidence(
	path string,
	readProblems func(string) (parsedProblemEvidence, error),
) (baselineProblemEvidence, error) {
	if path == "" {
		return baselineProblemEvidence{}, nil
	}

	parsed, err := readProblems(path)
	if errors.Is(err, os.ErrNotExist) {
		return baselineProblemEvidence{}, nil
	}
	if err != nil {
		return baselineProblemEvidence{}, err
	}
	if !parsed.Complete {
		return baselineProblemEvidence{}, nil
	}

	return baselineProblemEvidence{
		Available:  true,
		SourcePath: path,
		Problems:   parsed.Problems,
	}, nil
}

// selectBaselineProblemEvidence applies the report precedence contract:
// prefer VCR first, then NDJSON, then structured JUnit.
// VCR and NDJSON carry richer structured evidence; JUnit is last because its
// problems are recovered from failure text.
func selectBaselineProblemEvidence(
	vcr baselineProblemEvidence,
	ndjson baselineProblemEvidence,
	junit baselineProblemEvidence,
) baselineProblemEvidence {
	candidates := map[evidenceSource]baselineProblemEvidence{
		evidenceSourceVCR:    vcr,
		evidenceSourceNDJSON: ndjson,
		evidenceSourceJUnit:  junit,
	}
	for _, source := range problemEvidencePrecedence {
		evidence := candidates[source]
		if evidence.Available {
			return evidence
		}
	}

	return baselineProblemEvidence{}
}

func classifyCheckStatus(status string) checkStatus {
	switch strings.ToLower(status) {
	case "failure", "error":
		return checkStatusFailing
	case "success", "skip", "interrupted":
		return checkStatusPassing
	}

	return checkStatusUnrecognized
}

func categorizeCheckName(name string) checkCategory {
	category, ok := checkCategoriesByName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return checkCategoryUncategorized
	}

	return category
}

var checkCategoriesByName = map[string]checkCategory{
	"not_a_server_error":          checkCategoryServerError,
	"server error":                checkCategoryServerError,
	"negative_data_rejection":     checkCategoryNegativeDataRejection,
	"response_schema_conformance": checkCategoryResponseSchemaConformance,
	"response violates schema":    checkCategoryResponseSchemaConformance,
	"positive_data_acceptance":    checkCategoryPositiveDataAcceptance,
	"status_code_conformance":     checkCategoryStatusCodeConformance,
}
