package comparison

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

func classifyProblem(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) problemClassification {
	// Problem outcome precedence:
	// 1. uncategorized check => inconclusive tool limitation
	// 2. candidate 5xx + server-error check => still_failing
	// 3. matching precondition heuristic => generated_resource_precondition_loss
	// 4. category-specific evidence => fixed, still_failing, or inconclusive
	//
	// Rank 2 cannot overlap rank 3 in valid configuration: precondition
	// heuristics only apply to configured missing-resource statuses, and config
	// validation excludes 5xx from that set.
	if problem.CheckCategory == "" {
		problem.CheckCategory = categorizeCheckName(problem.CheckName)
	}
	if problem.CheckCategory == checkCategoryUncategorized {
		return problemClassification{
			outcome:       problemOutcomeInconclusive,
			outcomeReason: problemOutcomeReasonNoCategorizerForCheck,
		}
	}

	if isServerErrorStatus(interaction.CandidateResponse.Status) {
		if problem.CheckCategory == checkCategoryServerError {
			return problemClassification{outcome: problemOutcomeStillFailing}
		}

		return problemClassification{
			outcome:       problemOutcomeInconclusive,
			outcomeReason: problemOutcomeReasonChangedOutcome,
		}
	}

	heuristic, matched := matchingPreconditionHeuristic(policy, interaction)
	if matched {
		return problemClassification{
			outcome:                      problemOutcomeInconclusive,
			outcomeReason:                problemOutcomeReasonGeneratedResourcePreconditionLoss,
			exerciseEvidence:             exerciseEvidenceWithPreconditionLoss(problem, interaction, policy),
			matchedPreconditionHeuristic: heuristic.Name,
		}
	}

	classifyCategory, ok := problemClassifiersByCategory[problem.CheckCategory]
	if !ok {
		return problemClassification{
			outcome:       problemOutcomeInconclusive,
			outcomeReason: problemOutcomeReasonNoCategorizerForCheck,
		}
	}

	return classifyCategory(problem, interaction, policy)
}

type problemClassifier func(
	baselineProblem,
	reportInteractionEvidence,
	PreconditionPolicy,
) problemClassification

var problemClassifiersByCategory = map[checkCategory]problemClassifier{
	checkCategoryServerError:               classifyServerErrorProblem,
	checkCategoryNegativeDataRejection:     classifyNegativeDataRejectionProblem,
	checkCategoryPositiveDataAcceptance:    classifyPositiveDataAcceptanceProblem,
	checkCategoryResponseSchemaConformance: classifyResponseSchemaProblem,
}

func classifyServerErrorProblem(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) problemClassification {
	evidence := exerciseEvidenceWithNoPreconditionLoss(problem, interaction, policy)
	if hasExerciseEvidence(evidence) {
		return problemClassification{
			outcome:          problemOutcomeFixed,
			exerciseEvidence: evidence,
		}
	}

	return problemClassification{
		outcome:          problemOutcomeInconclusive,
		outcomeReason:    problemOutcomeReasonExerciseEvidenceMissing,
		exerciseEvidence: evidence,
	}
}

func classifyNegativeDataRejectionProblem(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) problemClassification {
	if isSuccessStatus(interaction.CandidateResponse.Status) {
		return problemClassification{
			outcome:       problemOutcomeStillFailing,
			outcomeReason: problemOutcomeReasonAcceptedInvalidData,
		}
	}

	if isClientErrorStatus(interaction.CandidateResponse.Status) {
		evidence := exerciseEvidenceWithNoPreconditionLoss(problem, interaction, policy)
		if hasRequestExerciseEvidence(evidence) {
			return problemClassification{
				outcome:          problemOutcomeFixed,
				outcomeReason:    problemOutcomeReasonValidationRejection,
				exerciseEvidence: evidence,
			}
		}
		return problemClassification{
			outcome:          problemOutcomeInconclusive,
			outcomeReason:    problemOutcomeReasonExerciseEvidenceMissing,
			exerciseEvidence: evidence,
		}
	}

	return problemClassification{
		outcome:       problemOutcomeInconclusive,
		outcomeReason: problemOutcomeReasonChangedOutcome,
	}
}

func classifyPositiveDataAcceptanceProblem(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) problemClassification {
	if isSuccessStatus(interaction.CandidateResponse.Status) {
		evidence := exerciseEvidenceWithNoPreconditionLoss(problem, interaction, policy)
		if hasRequestExerciseEvidence(evidence) {
			return problemClassification{
				outcome:          problemOutcomeFixed,
				outcomeReason:    problemOutcomeReasonAcceptedPositiveData,
				exerciseEvidence: evidence,
			}
		}
		return problemClassification{
			outcome:          problemOutcomeInconclusive,
			outcomeReason:    problemOutcomeReasonExerciseEvidenceMissing,
			exerciseEvidence: evidence,
		}
	}

	if interaction.CandidateResponse.Status == http.StatusConflict {
		return problemClassification{
			outcome:       problemOutcomeInconclusive,
			outcomeReason: problemOutcomeReasonStateConflict,
		}
	}

	if isClientErrorStatus(interaction.CandidateResponse.Status) {
		return problemClassification{
			outcome:       problemOutcomeStillFailing,
			outcomeReason: problemOutcomeReasonRepeatedRejection,
		}
	}

	return problemClassification{
		outcome:       problemOutcomeInconclusive,
		outcomeReason: problemOutcomeReasonChangedOutcome,
	}
}

func classifyResponseSchemaProblem(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) problemClassification {
	if normalizedResponseBodiesMatch(interaction, policy.Normalization) {
		return problemClassification{
			outcome:       problemOutcomeStillFailing,
			outcomeReason: problemOutcomeReasonRepeatedSchemaViolation,
		}
	}

	evidence := exerciseEvidenceWithNoPreconditionLoss(problem, interaction, policy)
	if sameStatusTransition(interaction) &&
		normalizedResponseBodiesDiffer(interaction, policy.Normalization) &&
		hasRequestExerciseEvidence(evidence) {
		return problemClassification{
			outcome:          problemOutcomeFixed,
			outcomeReason:    problemOutcomeReasonSchemaResponseChanged,
			exerciseEvidence: evidence,
		}
	}

	reason := problemOutcomeReasonSchemaValidationUnavailable
	if !sameStatusTransition(interaction) {
		reason = problemOutcomeReasonChangedOutcome
	}

	return problemClassification{
		outcome:          problemOutcomeInconclusive,
		outcomeReason:    reason,
		exerciseEvidence: evidence,
	}
}

func exerciseEvidenceWithPreconditionLoss(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) []string {
	return exerciseEvidence(problem, interaction, policy.Normalization)
}

func exerciseEvidenceWithNoPreconditionLoss(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	policy PreconditionPolicy,
) []string {
	evidence := exerciseEvidence(problem, interaction, policy.Normalization)
	evidence = append(evidence, exerciseEvidenceNoPreconditionLoss)

	return evidence
}

func exerciseEvidence(
	problem baselineProblem,
	interaction reportInteractionEvidence,
	normalization ResponseNormalizationConfig,
) []string {
	var evidence []string
	if sameProblemOperationAndPath(problem, interaction) {
		evidence = append(evidence, exerciseEvidenceOperationAndPathMatch)
	}
	if normalizedResponseBodiesMatch(interaction, normalization) {
		evidence = append(evidence, exerciseEvidenceNormalizedResponseMatch)
	}

	return evidence
}

func hasExerciseEvidence(evidence []string) bool {
	return slices.Contains(evidence, exerciseEvidenceOperationAndPathMatch) &&
		slices.Contains(evidence, exerciseEvidenceNormalizedResponseMatch) &&
		slices.Contains(evidence, exerciseEvidenceNoPreconditionLoss)
}

func hasRequestExerciseEvidence(evidence []string) bool {
	return slices.Contains(evidence, exerciseEvidenceOperationAndPathMatch) &&
		slices.Contains(evidence, exerciseEvidenceNoPreconditionLoss)
}

func sameProblemOperationAndPath(
	problem baselineProblem,
	interaction reportInteractionEvidence,
) bool {
	if problem.Reproduction.Method == "" && problem.Reproduction.URL == "" {
		return false
	}
	if problem.Reproduction.Method != "" &&
		!strings.EqualFold(problem.Reproduction.Method, interaction.Request.Method) {
		return false
	}
	if problem.Reproduction.URL == "" {
		return true
	}

	reproductionURL, err := url.Parse(problem.Reproduction.URL)
	if err != nil {
		return false
	}
	requestURL, err := url.Parse(interaction.Request.URL)
	if err != nil {
		return false
	}

	return reproductionURL.Path == requestURL.Path
}

func normalizedResponseBodiesMatch(
	interaction reportInteractionEvidence,
	normalization ResponseNormalizationConfig,
) bool {
	baseline, candidate, ok := normalizedResponseBodies(interaction, normalization)
	return ok && baseline == candidate
}

func normalizedResponseBodiesDiffer(
	interaction reportInteractionEvidence,
	normalization ResponseNormalizationConfig,
) bool {
	baseline, candidate, ok := normalizedResponseBodies(interaction, normalization)
	return ok && baseline != candidate
}

func normalizedResponseBodies(
	interaction reportInteractionEvidence,
	normalization ResponseNormalizationConfig,
) (string, string, bool) {
	if interaction.BaselineResponse == nil {
		return "", "", false
	}

	normalizedBaseline, ok := normalizedResponseBody(
		*interaction.BaselineResponse,
		normalization,
	)
	if !ok {
		return "", "", false
	}
	normalizedCandidate, ok := normalizedResponseBody(
		interaction.CandidateResponse,
		normalization,
	)
	if !ok {
		return "", "", false
	}

	return normalizedBaseline, normalizedCandidate, true
}

func normalizedResponseBody(
	response reportResponse,
	normalization ResponseNormalizationConfig,
) (string, bool) {
	body := response.Body
	if body == "" {
		return "", false
	}
	normalized, err := NormalizeResponse(
		RecordedResponse{
			Status:  response.Status,
			Headers: response.Headers,
			Body:    &body,
		},
		normalization,
	)
	if err != nil || normalized.Body == nil {
		return "", false
	}

	return *normalized.Body, true
}

func sameStatusTransition(interaction reportInteractionEvidence) bool {
	return interaction.StatusTransition.Baseline != nil &&
		*interaction.StatusTransition.Baseline == interaction.CandidateResponse.Status
}

func matchingPreconditionHeuristic(
	policy PreconditionPolicy,
	interaction reportInteractionEvidence,
) (PreconditionHeuristic, bool) {
	if interaction.BaselineResponse == nil ||
		!isSuccessStatus(interaction.BaselineResponse.Status) ||
		!slices.Contains(policy.MissingResourceStatuses, interaction.CandidateResponse.Status) {
		return PreconditionHeuristic{}, false
	}

	requestURL, err := url.Parse(interaction.Request.URL)
	if err != nil {
		return PreconditionHeuristic{}, false
	}
	for _, heuristic := range policy.Heuristics {
		if !strings.EqualFold(interaction.Request.Method, heuristic.Method) {
			continue
		}
		if heuristic.pathPattern.MatchString(requestURL.Path) {
			return heuristic, true
		}
	}

	return PreconditionHeuristic{}, false
}

func isServerErrorCheck(name string) bool {
	return categorizeCheckName(name) == checkCategoryServerError
}

func isServerErrorStatus(status int) bool {
	return status >= 500 && status <= 599
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status <= 299
}

func isClientErrorStatus(status int) bool {
	return status >= 400 && status <= 499
}
