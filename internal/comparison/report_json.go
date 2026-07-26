package comparison

import (
	"encoding/json"
	"fmt"
	"os"
)

func writeJSONReport(path string, document report) error {
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode comparison JSON report: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write comparison JSON report: %w", err)
	}

	return nil
}
