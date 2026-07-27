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

type checkStatus int

const (
	checkStatusPassing checkStatus = iota
	checkStatusFailing
	checkStatusUnrecognized
)

type problemAccumulator struct {
	source       evidenceSource
	problems     []baselineProblem
	unrecognized int
}

func (a *problemAccumulator) observe(status string, build func() baselineProblem) {
	switch classifyCheckStatus(status) {
	case checkStatusFailing:
		problem := build()
		problem.EvidenceSource = a.source
		a.problems = append(a.problems, problem)
	case checkStatusUnrecognized:
		a.unrecognized++
	}
}

func (a problemAccumulator) evidence() parsedProblemEvidence {
	return parsedProblemEvidence{
		Problems: a.problems,
		Complete: a.unrecognized == 0,
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
