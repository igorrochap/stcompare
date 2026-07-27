package comparison

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func readJUnitProblemEvidence(path string) (parsedProblemEvidence, error) {
	accumulator := problemAccumulator{source: evidenceSourceJUnit}
	_, err := walkJUnitProblemElements(path, func(body string) {
		extracted := parseJUnitProblems(body)
		if len(extracted) == 0 {
			accumulator.markIncompleteExtraction()
			return
		}
		accumulator.observeProblems(extracted)
	})
	if err != nil {
		return parsedProblemEvidence{}, err
	}

	return accumulator.evidence(), nil
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

type junitProblemParser struct {
	problems          []baselineProblem
	caseID            string
	groupStart        int
	activeProblem     int
	detailLines       []string
	commandLines      []string
	collectingCommand bool
}

func parseJUnitProblems(body string) []baselineProblem {
	parser := junitProblemParser{activeProblem: -1}
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		parser.feed(line)
	}
	parser.finalizeProblem()
	parser.finalizeGroup()

	return parser.result()
}

func (p *junitProblemParser) feed(line string) {
	const caseIDLabel = "Test Case ID:"

	trimmed := strings.TrimSpace(line)
	if index := strings.Index(trimmed, caseIDLabel); index >= 0 {
		p.finalizeProblem()
		p.finalizeGroup()
		p.caseID = strings.TrimSpace(trimmed[index+len(caseIDLabel):])
		return
	}
	if p.collectingCommand {
		p.commandLines = append(p.commandLines, line)
		return
	}
	if strings.HasPrefix(trimmed, "- ") {
		p.finalizeProblem()
		p.problems = append(p.problems, baselineProblem{
			CheckName: strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")),
			CaseID:    p.caseID,
		})
		p.activeProblem = len(p.problems) - 1
		return
	}
	if trimmed == "Reproduce with:" {
		p.finalizeProblem()
		p.collectingCommand = true
		return
	}
	if p.activeProblem >= 0 {
		p.detailLines = append(p.detailLines, line)
	}
}

func (p *junitProblemParser) finalizeProblem() {
	if p.activeProblem < 0 {
		return
	}
	p.problems[p.activeProblem].Message = strings.TrimSpace(strings.Join(p.detailLines, "\n"))
	p.activeProblem = -1
	p.detailLines = nil
}

func (p *junitProblemParser) finalizeGroup() {
	command := strings.TrimSpace(strings.Join(p.commandLines, "\n"))
	for index := p.groupStart; index < len(p.problems); index++ {
		p.problems[index].Reproduction.Command = command
	}
	p.groupStart = len(p.problems)
	p.commandLines = nil
	p.collectingCommand = false
}

func (p junitProblemParser) result() []baselineProblem {
	return p.problems
}
