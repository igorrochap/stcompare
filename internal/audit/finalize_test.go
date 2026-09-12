package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalizePersistsActivitySummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark-audit.json")
	contents := []byte(`{
  "schema_version": "1",
  "run": {"id": "run-activity"},
  "capture": {"enabled": true, "status": "in_progress", "complete": false},
  "iterations": [{"id": "iteration-1", "number": 1, "turn_ids": []}],
  "events": [
    {"type": "model_tool_call", "iteration_id": "iteration-1", "status": "failed", "duration_ms": 12},
    {"type": "adapter_operation", "iteration_id": "iteration-1", "status": "failed", "duration_ms": 10}
  ]
}`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}
	if err := Finalize(path, "converged", time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), false, ""); err != nil {
		t.Fatalf("finalize audit: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read finalized audit: %v", err)
	}
	var document Artifact
	if err := json.Unmarshal(updated, &document); err != nil {
		t.Fatalf("decode finalized audit: %v", err)
	}
	if document.Activity.Status != "complete" || document.Activity.ModelToolCalls.Count != 1 || document.Activity.ModelToolCalls.Failed != 1 {
		t.Fatalf("final activity = %#v, want complete failed-call summary", document.Activity)
	}
	if document.Iterations[0].Activity.AdapterOperations.DurationMS != 0 {
		t.Fatalf("iteration activity = %#v, want no failed-operation duration", document.Iterations[0].Activity)
	}
}

func TestFinalizePreservesRawModelInputAndMarksPartial(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "benchmark-audit.json")
	contents := []byte(`{
  "schema_version": "1",
  "run": {"id": "run-1"},
  "capture": {"enabled": true, "status": "in_progress", "complete": false},
  "iterations": [],
  "events": [{"sequence": 1, "type": "model_turn", "status": "started", "input": { "temperature": 0, "messages": [1, 2] }}]
}`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	endedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := Finalize(path, "adapter_error", endedAt, true, "model timeout"); err != nil {
		t.Fatalf("finalize audit: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read finalized audit: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(updated, &document); err != nil {
		t.Fatalf("decode finalized audit: %v", err)
	}
	var capture map[string]json.RawMessage
	if err := json.Unmarshal(document["capture"], &capture); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	if string(capture["status"]) != `"partial"` || string(capture["complete"]) != "false" {
		t.Fatalf("capture = %s, want partial incomplete capture", capture["status"])
	}
	if !strings.Contains(string(updated), `"failure":"model timeout"`) {
		t.Fatalf("finalized audit missing failure: %s", updated)
	}
	if !strings.Contains(string(updated), `"temperature":0`) || !strings.Contains(string(updated), `"messages":[1,2]`) {
		t.Fatalf("finalized audit changed model input: %s", updated)
	}
}
