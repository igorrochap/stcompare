package comparison

import (
	"encoding/base64"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

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

	accumulator := problemAccumulator{source: evidenceSourceVCR}
	for _, interaction := range document.HTTPInteractions {
		if err := accumulateVCRInteractionProblems(&accumulator, interaction); err != nil {
			return parsedProblemEvidence{}, err
		}
	}

	return accumulator.evidence(), nil
}

func accumulateVCRInteractionProblems(
	accumulator *problemAccumulator,
	interaction vcrInteraction,
) error {
	body, err := vcrRequestBody(interaction.Request.Body)
	if err != nil {
		return err
	}
	headers := flattenHeaderMap(interaction.Request.Headers)

	for _, check := range interaction.Checks {
		accumulator.observe(check.Status, func() baselineProblem {
			return baselineProblem{
				CheckName: check.Name,
				Message:   check.Message,
				CaseID:    interaction.ID,
				Reproduction: newStructuredProblemReproduction(
					interaction.Request.Method,
					interaction.Request.URI,
					headers,
					body,
				),
			}
		})
	}

	return nil
}

func vcrRequestBody(body vcrBody) ([]byte, error) {
	requestBody := []byte(body.String)
	if body.Base64String == nil {
		return requestBody, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(*body.Base64String)
	if err != nil {
		return nil, fmt.Errorf("decode baseline VCR request body: %w", err)
	}

	return decoded, nil
}
