package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeActivityCountsCallsAndAdapterOperationsByIteration(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Iterations: []Iteration{
			{ID: "iteration-1", Number: 1},
			{ID: "iteration-2", Number: 2},
		},
		Events: []Event{
			{Type: "model_tool_call", IterationID: "iteration-1", Status: "completed", DurationMS: 11},
			{Type: "model_tool_call", IterationID: "iteration-1", Status: "failed", DurationMS: 7},
			{Type: "model_tool_call", IterationID: "iteration-1", Status: "started"},
			{Type: "adapter_operation", IterationID: "iteration-1", Status: "completed", DurationMS: 5},
			{Type: "adapter_operation", IterationID: "iteration-1", Status: "failed", DurationMS: 3},
			{Type: "model_tool_call", IterationID: "iteration-2", Status: "completed", DurationMS: 13},
			{Type: "adapter_operation", IterationID: "iteration-2", Status: "completed", DurationMS: 9},
		},
	}

	summary := SummarizeActivity(document)
	if summary.Status != "partial" {
		t.Fatalf("activity status = %q, want partial for started evidence", summary.Status)
	}
	if got := summary.ModelToolCalls; got.Count != 4 || got.Completed != 2 || got.Failed != 1 || got.Incomplete != 1 || got.DurationMS != 24 {
		t.Fatalf("model tool calls = %#v, want four calls with complete, failed, and incomplete outcomes", got)
	}
	if got := summary.AdapterOperations; got.Count != 3 || got.Completed != 2 || got.Failed != 1 || got.Incomplete != 0 || got.DurationMS != 14 {
		t.Fatalf("adapter operations = %#v, want three separate operations", got)
	}

	iteration := summarizeIterationActivity(document, "iteration-1")
	if iteration.ModelToolCalls.Count != 3 || iteration.AdapterOperations.Count != 2 {
		t.Fatalf("iteration activity = %#v, want iteration-1-only counts", iteration)
	}
}

func TestSummarizeActivitySeparatesEditAttemptsFromFileModifications(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Iterations: []Iteration{
			{ID: "iteration-1", Number: 1},
			{ID: "iteration-2", Number: 2},
		},
		Events: []Event{
			{Type: "model_tool_call", ToolName: "str_replace", EditAttempt: true, IterationID: "iteration-1", Status: "completed"},
			{Type: "model_tool_call", ToolName: "write_file", EditAttempt: true, IterationID: "iteration-1", Status: "completed"},
			{Type: "model_tool_call", ToolName: "str_replace", EditAttempt: true, IterationID: "iteration-1", Status: "failed"},
			{Type: "model_tool_call", ToolName: "read_file", IterationID: "iteration-2", Status: "completed"},
			{Type: "model_tool_call", ToolName: "str_replace", IterationID: "iteration-2", Status: "completed"},
			{Type: "adapter_operation", EditAttempt: true, IterationID: "iteration-2", Status: "completed"},
		},
		FileModifications: []FileModification{
			{ID: "file-modification-1", Path: "api.py", IterationID: "iteration-1"},
			{ID: "file-modification-2", Path: "new.py", IterationID: "iteration-1"},
		},
	}

	summary := SummarizeActivity(document)
	if summary.EditAttempts != 4 || summary.FileModifications != 2 {
		t.Fatalf("edit activity = %#v, want four attempts and two modifications", summary)
	}
	iteration := summarizeIterationActivity(document, "iteration-1")
	if iteration.EditAttempts != 3 || iteration.FileModifications != 2 {
		t.Fatalf("iteration edit activity = %#v, want iteration-1 counts", iteration)
	}
	if got := summarizeIterationActivity(document, "iteration-2"); got.EditAttempts != 1 || got.FileModifications != 0 {
		t.Fatalf("iteration-2 edit activity = %#v, want one patch attempt", got)
	}
}

func TestReadDoesNotInventCompleteZeroForMissingActivityEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark-audit.json")
	contents := []byte(`{
  "schema_version": "1",
  "capture": {"enabled": true, "status": "complete", "complete": true},
  "iterations": [],
  "events": [{"type": "model_turn", "status": "completed"}]
}`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}

	document, err := Read(path)
	if err != nil {
		t.Fatalf("read audit fixture: %v", err)
	}
	if document.Activity.Status != "not_reported" || document.Activity.ModelToolCalls.Count != 0 {
		t.Fatalf("activity = %#v, want not-reported evidence", document.Activity)
	}
	html, err := Render(document)
	if err != nil {
		t.Fatalf("render audit fixture: %v", err)
	}
	if !strings.Contains(html, "Activity evidence: not reported.") ||
		strings.Contains(html, "Model Tool Calls</span><strong>0</strong>") {
		t.Fatalf("rendered audit misrepresented missing activity evidence:\n%s", html)
	}
}
