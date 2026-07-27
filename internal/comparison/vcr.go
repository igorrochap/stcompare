package comparison

import (
	"encoding/base64"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type baselineProblem struct {
	CheckName         string              `json:"check_name"`
	Message           string              `json:"message"`
	EvidenceSource    string              `json:"evidence_source"`
	CaseID            string              `json:"case_id"`
	CorrelationStatus correlationStatus   `json:"correlation_status"`
	Reproduction      problemReproduction `json:"reproduction"`
	Interaction       *int                `json:"interaction"`
}

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

type vcrDocument struct {
	HTTPInteractions []vcrInteraction `yaml:"http_interactions"`
}

type vcrInteraction struct {
	ID      string     `yaml:"id"`
	Checks  []vcrCheck `yaml:"checks"`
	Request vcrRequest `yaml:"request"`
}

type vcrCheck struct {
	Name    string `yaml:"name"`
	Status  string `yaml:"status"`
	Message string `yaml:"message"`
}

type vcrRequest struct {
	URI     string              `yaml:"uri"`
	Method  string              `yaml:"method"`
	Headers map[string][]string `yaml:"headers"`
	Body    vcrBody             `yaml:"body"`
}

type vcrBody struct {
	String       string  `yaml:"string"`
	Base64String *string `yaml:"base64_string"`
}

func readVCRProblems(path string) (parsedProblemEvidence, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return parsedProblemEvidence{}, fmt.Errorf("read baseline VCR: %w", err)
	}

	var document vcrDocument
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return parsedProblemEvidence{}, fmt.Errorf("decode baseline VCR: %w", err)
	}

	var problems []baselineProblem
	failingChecks := 0
	for _, interaction := range document.HTTPInteractions {
		body := []byte(interaction.Request.Body.String)
		if interaction.Request.Body.Base64String != nil {
			decoded, err := base64.StdEncoding.DecodeString(*interaction.Request.Body.Base64String)
			if err != nil {
				return parsedProblemEvidence{}, fmt.Errorf("decode baseline VCR request body: %w", err)
			}
			body = decoded
		}
		headers := flattenHeaderMap(interaction.Request.Headers)

		for _, check := range interaction.Checks {
			if !isFailingCheckStatus(check.Status) {
				continue
			}
			failingChecks++
			problems = append(problems, baselineProblem{
				CheckName:      check.Name,
				Message:        check.Message,
				EvidenceSource: "vcr",
				CaseID:         interaction.ID,
				Reproduction: problemReproduction{
					Method:  interaction.Request.Method,
					URL:     interaction.Request.URI,
					Headers: headers,
					Body:    string(body),
				},
			})
		}
	}

	return parsedProblemEvidence{
		Problems: problems,
		Complete: failingChecks == 0 || len(problems) != 0,
	}, nil
}
