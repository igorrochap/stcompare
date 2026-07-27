package comparison

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func readJUnitProblemEvidence(path string) (baselineProblemEvidence, error) {
	var problems []baselineProblem
	complete := true
	_, err := walkJUnitProblemElements(path, func(body string) {
		extracted := parseJUnitProblems(body)
		if len(extracted) == 0 {
			complete = false
			return
		}
		problems = append(problems, extracted...)
	})
	if err != nil {
		return baselineProblemEvidence{}, err
	}

	if !complete {
		return baselineProblemEvidence{}, nil
	}

	return baselineProblemEvidence{
		Available: true,
		Problems:  problems,
	}, nil
}

func readJUnitProblemCount(path string) (*int, error) {
	count, err := walkJUnitProblemElements(path, func(string) {})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &count, nil
}

func walkJUnitProblemElements(path string, visit func(string)) (int, error) {
	document, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("read baseline JUnit: %w", err)
	}
	defer func() {
		_ = document.Close()
	}()

	count := 0
	decoder := xml.NewDecoder(document)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("decode baseline JUnit: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok || !isJUnitProblemElement(start) {
			continue
		}
		count++

		var body string
		if err := decoder.DecodeElement(&body, &start); err != nil {
			return 0, fmt.Errorf("decode baseline JUnit: %w", err)
		}
		visit(body)
	}

	return count, nil
}

func isJUnitProblemElement(start xml.StartElement) bool {
	return start.Name.Local == "failure" || start.Name.Local == "error"
}

func parseJUnitProblems(body string) []baselineProblem {
	const caseIDLabel = "Test Case ID:"

	var (
		problems          []baselineProblem
		caseID            string
		groupStart        int
		activeProblem     = -1
		detailLines       []string
		commandLines      []string
		collectingCommand bool
	)
	finalizeProblem := func() {
		if activeProblem < 0 {
			return
		}
		problems[activeProblem].Message = strings.TrimSpace(strings.Join(detailLines, "\n"))
		activeProblem = -1
		detailLines = nil
	}
	finalizeGroup := func() {
		command := strings.TrimSpace(strings.Join(commandLines, "\n"))
		for index := groupStart; index < len(problems); index++ {
			problems[index].Reproduction.Command = command
		}
		groupStart = len(problems)
		commandLines = nil
		collectingCommand = false
	}

	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		trimmed := strings.TrimSpace(line)
		if index := strings.Index(trimmed, caseIDLabel); index >= 0 {
			finalizeProblem()
			finalizeGroup()
			caseID = strings.TrimSpace(trimmed[index+len(caseIDLabel):])
			continue
		}
		if collectingCommand {
			commandLines = append(commandLines, line)
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			finalizeProblem()
			problems = append(problems, baselineProblem{
				CheckName:      strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")),
				EvidenceSource: "junit",
				CaseID:         caseID,
			})
			activeProblem = len(problems) - 1
			continue
		}
		if trimmed == "Reproduce with:" {
			finalizeProblem()
			collectingCommand = true
			continue
		}
		if activeProblem >= 0 {
			detailLines = append(detailLines, line)
		}
	}
	finalizeProblem()
	finalizeGroup()

	return problems
}
