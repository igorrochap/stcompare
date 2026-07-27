package comparison

import (
	"errors"
	"os"
)

func readOptionalProblemEvidence(
	path string,
	readProblems func(string) ([]baselineProblem, error),
) (baselineProblemEvidence, error) {
	if path == "" {
		return baselineProblemEvidence{}, nil
	}

	problems, err := readProblems(path)
	if errors.Is(err, os.ErrNotExist) {
		return baselineProblemEvidence{}, nil
	}
	if err != nil {
		return baselineProblemEvidence{}, err
	}

	return baselineProblemEvidence{
		Available: true,
		Source:    path,
		Problems:  problems,
	}, nil
}

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
