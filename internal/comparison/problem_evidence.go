package comparison

import (
	"errors"
	"os"
	"strings"
)

type parsedProblemEvidence struct {
	Problems []baselineProblem
	Complete bool
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
		Available: true,
		Source:    path,
		Problems:  parsed.Problems,
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
	if vcr.Available {
		return vcr
	}
	if ndjson.Available {
		return ndjson
	}
	if junit.Available {
		return junit
	}

	return baselineProblemEvidence{}
}

func isFailingCheckStatus(status string) bool {
	return strings.EqualFold(status, "failure")
}
