package comparison

import (
	"encoding/json"
	"fmt"
	"os"
)

func readHAREntries(path string) ([]harEntry, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline HAR: %w", err)
	}

	var document harDocument
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("decode baseline HAR: %w", err)
	}

	for index, entry := range document.Log.Entries {
		if entry.Request.PostData.Encoding != "" {
			return nil, fmt.Errorf(
				"request %d postData encoding %q is unsupported",
				index+1,
				entry.Request.PostData.Encoding,
			)
		}
	}

	return document.Log.Entries, nil
}
