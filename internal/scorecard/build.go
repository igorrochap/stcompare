package scorecard

import (
	"encoding/json"
	"fmt"
	"os"

	"stcompare/benchrecord"
	"stcompare/internal/comparison"
)

// Input identifies the artifacts joined into a scorecard and its output path.
type Input struct {
	ComparisonPath string
	RecordPath     string
	OutputPath     string
}

// Build reads the source artifacts and writes their joined HTML scorecard.
func Build(input Input) error {
	comparisonContents, err := os.ReadFile(input.ComparisonPath)
	if err != nil {
		return fmt.Errorf("read comparison file %q: %w", input.ComparisonPath, err)
	}
	var document comparison.Report
	if err := json.Unmarshal(comparisonContents, &document); err != nil {
		return fmt.Errorf("parse comparison file %q: %w", input.ComparisonPath, err)
	}

	recordContents, err := os.ReadFile(input.RecordPath)
	if err != nil {
		return fmt.Errorf("read benchmark record file %q: %w", input.RecordPath, err)
	}
	var record benchrecord.Record
	if err := json.Unmarshal(recordContents, &record); err != nil {
		return fmt.Errorf("parse benchmark record file %q: %w", input.RecordPath, err)
	}

	html, err := Render(document, record)
	if err != nil {
		return err
	}
	if err := os.WriteFile(input.OutputPath, []byte(html), 0o644); err != nil {
		return fmt.Errorf("write scorecard file %q: %w", input.OutputPath, err)
	}

	return nil
}
