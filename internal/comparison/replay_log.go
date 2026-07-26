package comparison

import (
	"encoding/json"
	"fmt"
	"os"
)

func writeReplayResponseLog(path string, entries []harEntry) error {
	document := harDocument{
		Log: harLog{
			Version: harVersion,
			Entries: entries,
		},
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode replay response log: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write replay response log: %w", err)
	}

	return nil
}
