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
	accumulator := problemAccumulator{source: evidenceSourceNDJSON}
	if err := walkNDJSONScenarios(path, func(recorder ndjsonRecorder) error {
		return accumulateNDJSONRecorderProblems(&accumulator, recorder)
	}); err != nil {
		return parsedProblemEvidence{}, err
	}

	return accumulator.evidence(), nil
}

func walkNDJSONScenarios(path string, visit func(ndjsonRecorder) error) error {
	document, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read baseline NDJSON: %w", err)
	}
	defer func() {
		_ = document.Close()
	}()

	reader := bufio.NewReader(document)
	for lineNumber := 1; ; lineNumber++ {
		line, readErr := reader.ReadBytes('\n')
		// ReadBytes returns the last line's data together with io.EOF when the
		// file has no trailing newline, so the line must be processed before the
		// EOF check.
		if len(bytes.TrimSpace(line)) != 0 {
			var event ndjsonEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return fmt.Errorf("decode baseline NDJSON line %d: %w", lineNumber, err)
			}
			if event.ScenarioFinished != nil {
				if err := visit(event.ScenarioFinished.Recorder); err != nil {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read baseline NDJSON line %d: %w", lineNumber, readErr)
		}
	}

	return nil
}

func accumulateNDJSONRecorderProblems(
	accumulator *problemAccumulator,
	recorder ndjsonRecorder,
) error {
	caseIDs := make([]string, 0, len(recorder.Checks))
	for caseID := range recorder.Checks {
		caseIDs = append(caseIDs, caseID)
	}
	sort.Strings(caseIDs)

	for _, caseID := range caseIDs {
		interaction := recorder.Interactions[caseID]
		body, err := base64.StdEncoding.DecodeString(interaction.Request.Body.Base64)
		if err != nil {
			return fmt.Errorf(
				"decode baseline NDJSON request body for case %q: %w",
				caseID,
				err,
			)
		}
		headers := flattenHeaderMap(interaction.Request.Headers)

		for _, check := range recorder.Checks[caseID] {
			message := ndjsonCheckMessage(check)
			accumulator.observe(check.Status, func() baselineProblem {
				return baselineProblem{
					CheckName: check.Name,
					Message:   message,
					CaseID:    caseID,
					Reproduction: problemReproduction{
						Method:  interaction.Request.Method,
						URL:     interaction.Request.URI,
						Headers: headers,
						Body:    string(body),
					},
				}
			})
		}
	}

	return nil
}

func ndjsonCheckMessage(check ndjsonCheck) string {
	if check.FailureInfo == nil {
		return ""
	}

	message := check.FailureInfo.Failure.Message
	if message != "" {
		return message
	}

	return check.FailureInfo.Failure.Title
}
