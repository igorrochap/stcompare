package comparison

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
)

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
