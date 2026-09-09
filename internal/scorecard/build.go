package scorecard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"stcompare/benchrecord"
	"stcompare/internal/audit"
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
	auditDocument := loadAuditDocument(input.RecordPath, &record)

	html, err := RenderWithAudit(document, record, auditDocument)
	if err != nil {
		return err
	}
	if err := os.WriteFile(input.OutputPath, []byte(html), 0o644); err != nil {
		return fmt.Errorf("write scorecard file %q: %w", input.OutputPath, err)
	}

	return nil
}

func loadAuditDocument(recordPath string, record *benchrecord.Record) *audit.Artifact {
	if record.Audit.Artifact == "" {
		return nil
	}
	auditPath := filepath.Join(filepath.Dir(recordPath), record.Audit.Artifact)
	document, err := audit.Read(auditPath)
	if err != nil {
		return nil
	}
	if record.Audit.Activity == nil {
		activity := document.Activity
		record.Audit.Activity = &activity
	}
	return &document
}
