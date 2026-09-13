package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Finalize marks an existing audit with the benchmark's terminal state. The
// update preserves raw event JSON so exact model inputs and responses remain
// reconstructable after finalization.
func Finalize(path string, terminalState string, endedAt time.Time, partial bool, failure string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read audit artifact for finalization: %w", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse audit artifact for finalization: %w", err)
	}

	capture, err := rawObject(document, "capture")
	if err != nil {
		return fmt.Errorf("finalize audit capture: %w", err)
	}
	captureStatus := "complete"
	if partial {
		captureStatus = "partial"
	}
	capture["status"] = rawString(captureStatus)
	capture["complete"] = rawBool(!partial)
	capture["finished_at"] = rawString(endedAt.UTC().Format(time.RFC3339Nano))
	if failure != "" {
		capture["failure"] = rawString(failure)
	}
	document["capture"] = mustMarshal(capture)

	run, err := rawObject(document, "run")
	if err != nil {
		return fmt.Errorf("finalize audit run: %w", err)
	}
	run["ended_at"] = rawString(endedAt.UTC().Format(time.RFC3339Nano))
	run["terminal_state"] = rawString(terminalState)
	document["run"] = mustMarshal(run)

	if err := refreshRawActivity(document); err != nil {
		return fmt.Errorf("finalize audit activity: %w", err)
	}
	if err := writeAtomically(path, mustMarshal(document)); err != nil {
		return fmt.Errorf("write finalized audit artifact: %w", err)
	}
	return nil
}

func refreshRawActivity(document map[string]json.RawMessage) error {
	var artifact Artifact
	if err := json.Unmarshal(mustMarshal(document), &artifact); err != nil {
		return fmt.Errorf("parse activity events: %w", err)
	}
	document["activity"] = mustMarshal(SummarizeActivity(artifact))
	document["efficiency"] = mustMarshal(SummarizeEfficiency(artifact))

	rawIterations, ok := document["iterations"]
	if !ok {
		return nil
	}
	var iterations []json.RawMessage
	if err := json.Unmarshal(rawIterations, &iterations); err != nil {
		return fmt.Errorf("parse activity iterations: %w", err)
	}
	for index, rawIteration := range iterations {
		var iteration map[string]json.RawMessage
		if err := json.Unmarshal(rawIteration, &iteration); err != nil {
			return fmt.Errorf("parse activity iteration %d: %w", index, err)
		}
		var iterationID string
		if rawID, exists := iteration["id"]; exists {
			if err := json.Unmarshal(rawID, &iterationID); err != nil {
				return fmt.Errorf("parse activity iteration %d identity: %w", index, err)
			}
		}
		iterationActivity := summarizeIterationActivity(artifact, iterationID)
		iteration["activity"] = mustMarshal(iterationActivity)
		iteration["efficiency"] = mustMarshal(summarizeIterationEfficiency(artifact, iterationID))
		iterations[index] = mustMarshal(iteration)
	}
	document["iterations"] = mustMarshal(iterations)
	return nil
}

func rawObject(document map[string]json.RawMessage, name string) (map[string]json.RawMessage, error) {
	raw, ok := document[name]
	if !ok {
		return nil, fmt.Errorf("missing %s object", name)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func rawString(value string) json.RawMessage {
	return mustMarshal(value)
}

func rawBool(value bool) json.RawMessage {
	return mustMarshal(value)
}

func mustMarshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal audit value: %v", err))
	}
	return encoded
}

func writeAtomically(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".audit-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}
