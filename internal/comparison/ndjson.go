package comparison

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type ndjsonEvent struct {
	ScenarioFinished *ndjsonScenarioFinished `json:"ScenarioFinished"`
}

type ndjsonScenarioFinished struct {
	Recorder ndjsonRecorder `json:"recorder"`
}

type ndjsonRecorder struct {
	Checks       map[string][]ndjsonCheck     `json:"checks"`
	Interactions map[string]ndjsonInteraction `json:"interactions"`
}

type ndjsonCheck struct {
	Name        string                  `json:"name"`
	Status      string                  `json:"status"`
	FailureInfo *ndjsonCheckFailureInfo `json:"failure_info"`
}

type ndjsonCheckFailureInfo struct {
	Failure ndjsonFailure `json:"failure"`
}

type ndjsonFailure struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type ndjsonInteraction struct {
	Request ndjsonRequest `json:"request"`
}

type ndjsonRequest struct {
	Method  string              `json:"method"`
	URI     string              `json:"uri"`
	Headers map[string][]string `json:"headers"`
	Body    ndjsonBody          `json:"body"`
}

type ndjsonBody struct {
	Base64 string `json:"$base64"`
}

func readNDJSONProblems(path string) (parsedProblemEvidence, error) {
	document, err := os.Open(path)
	if err != nil {
		return parsedProblemEvidence{}, fmt.Errorf("read baseline NDJSON: %w", err)
	}
	defer func() {
		_ = document.Close()
	}()

	var problems []baselineProblem
	failingChecks := 0
	reader := bufio.NewReader(document)
	for lineNumber := 1; ; lineNumber++ {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) != 0 {
			var event ndjsonEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return parsedProblemEvidence{}, fmt.Errorf("decode baseline NDJSON line %d: %w", lineNumber, err)
			}
			if event.ScenarioFinished != nil {
				recorder := event.ScenarioFinished.Recorder
				caseIDs := make([]string, 0, len(recorder.Checks))
				for caseID := range recorder.Checks {
					caseIDs = append(caseIDs, caseID)
				}
				sort.Strings(caseIDs)

				for _, caseID := range caseIDs {
					interaction := recorder.Interactions[caseID]
					body, err := base64.StdEncoding.DecodeString(interaction.Request.Body.Base64)
					if err != nil {
						return parsedProblemEvidence{}, fmt.Errorf(
							"decode baseline NDJSON request body for case %q: %w",
							caseID,
							err,
						)
					}
					headers := flattenNDJSONHeaders(interaction.Request.Headers)

					for _, check := range recorder.Checks[caseID] {
						if !isFailingCheckStatus(check.Status) {
							continue
						}
						failingChecks++

						message := ""
						if check.FailureInfo != nil {
							message = check.FailureInfo.Failure.Message
							if message == "" {
								message = check.FailureInfo.Failure.Title
							}
						}
						problems = append(problems, baselineProblem{
							CheckName:      check.Name,
							Message:        message,
							EvidenceSource: "ndjson",
							CaseID:         caseID,
							Reproduction: problemReproduction{
								Method:  interaction.Request.Method,
								URL:     interaction.Request.URI,
								Headers: headers,
								Body:    string(body),
							},
						})
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return parsedProblemEvidence{}, fmt.Errorf("read baseline NDJSON line %d: %w", lineNumber, readErr)
		}
	}

	return parsedProblemEvidence{
		Problems: problems,
		Complete: failingChecks == 0 || len(problems) != 0,
	}, nil
}

func flattenNDJSONHeaders(headers map[string][]string) []harHeader {
	var flattened []harHeader
	for name, values := range headers {
		for _, value := range values {
			flattened = append(flattened, harHeader{Name: name, Value: value})
		}
	}

	return sortedHARHeaders(flattened)
}
