package comparison

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func readJUnitProblems(path string) (parsedProblemEvidence, error) {
	problems, complete, err := readJUnitProblemEvidence(path)
	if err != nil {
		return parsedProblemEvidence{}, err
	}

	return parsedProblemEvidence{
		Problems: problems,
		Complete: complete,
	}, nil
}

func readJUnitProblemEvidence(path string) ([]baselineProblem, bool, error) {
	document, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("read baseline JUnit: %w", err)
	}
	defer func() {
		_ = document.Close()
	}()

	var problems []baselineProblem
	complete := true
	decoder := xml.NewDecoder(document)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("decode baseline JUnit: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok || (start.Name.Local != "failure" && start.Name.Local != "error") {
			continue
		}

		var body string
		if err := decoder.DecodeElement(&body, &start); err != nil {
			return nil, false, fmt.Errorf("decode baseline JUnit: %w", err)
		}
		extracted := parseJUnitProblems(body)
		if len(extracted) == 0 {
			complete = false
			continue
		}
		problems = append(problems, extracted...)
	}

	return problems, complete, nil
}

func readJUnitProblemCount(path string) (*int, error) {
	document, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read baseline JUnit: %w", err)
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
			return nil, fmt.Errorf("decode baseline JUnit: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if ok && (start.Name.Local == "failure" || start.Name.Local == "error") {
			count++
		}
	}

	return &count, nil
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
