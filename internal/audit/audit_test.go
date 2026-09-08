package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderShowsChronologicalTurnsAndModelRationale(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-1", Candidate: "candidate", Agent: "local-model", Model: "model"},
		Capture:       Capture{Enabled: true, Status: "in_progress"},
		Iterations:    []Iteration{{ID: "iteration-1", Number: 1, TurnIDs: []string{"iteration-1-turn-1"}}},
		Events: []Event{
			{
				Sequence:    1,
				Type:        "model_turn",
				IterationID: "iteration-1",
				Iteration:   1,
				TurnID:      "iteration-1-turn-1",
				Status:      "completed",
				Input:       json.RawMessage(`{"messages":[{"role":"system","content":"instruction"}]}`),
				Returned:    json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"I changed the handler because the response schema failed."}}]}`),
				ReturnedMessages: []json.RawMessage{
					json.RawMessage(`{"role":"assistant","content":"I changed the handler because the response schema failed."}`),
				},
			},
			{
				Sequence:    2,
				Type:        "model_turn",
				IterationID: "iteration-1",
				Iteration:   1,
				TurnID:      "iteration-1-turn-2",
				Status:      "started",
				Input:       json.RawMessage(`{"messages":[{"role":"tool","content":"result"}]}`),
			},
		},
	}

	html, err := Render(document)
	if err != nil {
		t.Fatalf("render audit: %v", err)
	}
	for _, fragment := range []string{
		"Model-turn audit",
		"partial",
		"run-1",
		"iteration-1-turn-1",
		"iteration-1-turn-2",
		"Exact model input",
		"instruction",
		"Model's stated rationale",
		"I changed the handler because the response schema failed.",
		"Partial turn: inference or capture did not complete.",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("audit HTML missing %q:\n%s", fragment, html)
		}
	}
	if strings.Index(html, "iteration-1-turn-1") > strings.Index(html, "iteration-1-turn-2") {
		t.Fatal("audit HTML does not preserve chronological turn order")
	}
}

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

func TestRenderShowsExpandableToolActivityAndIncompleteEvidence(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-activity"},
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Iterations:    []Iteration{{ID: "iteration-1", Number: 1}},
		Events: []Event{
			{
				Sequence:    1,
				Type:        "model_tool_call",
				ID:          "model-tool-call-1",
				Iteration:   1,
				IterationID: "iteration-1",
				TurnID:      "iteration-1-turn-1",
				ToolName:    "read_file",
				Arguments:   json.RawMessage(`{"path":"api.py"}`),
				Request:     json.RawMessage(`{"function":{"name":"read_file"}}`),
				Result:      json.RawMessage(`{"ok":true,"content":"source"}`),
				Status:      "completed",
				StartedAt:   "2026-01-01T00:00:00Z",
				EndedAt:     "2026-01-01T00:00:00.100Z",
				DurationMS:  100,
			},
			{
				Sequence:        2,
				Type:            "adapter_operation",
				ID:              "adapter-operation-2",
				Iteration:       1,
				IterationID:     "iteration-1",
				TurnID:          "iteration-1-turn-1",
				ModelToolCallID: "model-tool-call-1",
				Operation:       "execute_model_tool_call",
				Status:          "completed",
				DurationMS:      99,
			},
			{
				Sequence:    3,
				Type:        "model_tool_call",
				ID:          "model-tool-call-3",
				Iteration:   1,
				IterationID: "iteration-1",
				TurnID:      "iteration-1-turn-2",
				ToolName:    "write_file",
				Arguments:   json.RawMessage(`{"path":"new.py"}`),
				Status:      "started",
				StartedAt:   "2026-01-01T00:00:01Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("render activity audit: %v", err)
	}
	for _, fragment := range []string{
		"Activity summary",
		"Model Tool Calls",
		"Adapter Operations",
		"<details>",
		"Tool arguments",
		"api.py",
		"Tool result",
		"Execution time",
		"Incomplete Model Tool Call",
		"partial evidence",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("activity audit HTML missing %q:\n%s", fragment, html)
		}
	}
}

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

func TestRenderReconstructsSharedModelInput(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		SharedContent: map[string]json.RawMessage{
			"content-system": json.RawMessage(`"system instruction"`),
		},
		Events: []Event{{
			Sequence: 1,
			Type:     "model_turn",
			Status:   "completed",
			Input:    json.RawMessage(`{"messages":[{"role":"system","content":null}]}`),
			InputContentReferences: []ContentReference{{
				Path: "/messages/0/content",
				ID:   "content-system",
			}},
		}},
	}

	html, err := Render(document)
	if err != nil {
		t.Fatalf("render shared-content audit: %v", err)
	}
	if !strings.Contains(html, "system instruction") {
		t.Fatalf("rendered audit omitted reconstructed model input:\n%s", html)
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

func TestBuildWritesStandaloneAuditReport(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	outputPath := filepath.Join(directory, "benchmark-audit.html")
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-1"},
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Events:        []Event{},
	}
	contents, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(auditPath, contents, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := Build(auditPath, outputPath); err != nil {
		t.Fatalf("build audit report: %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("audit report: %v", err)
	}
}

func TestRenderMarksIncompleteCaptureAsPartial(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: false},
	})
	if err != nil {
		t.Fatalf("render incomplete audit: %v", err)
	}
	if !strings.Contains(html, `class="status partial"`) ||
		!strings.Contains(html, "This audit is partial") {
		t.Fatalf("incomplete audit was not rendered as partial:\n%s", html)
	}
}

func TestRenderShowsNotReportedWhenCaptureIsDisabled(t *testing.T) {
	html, err := Render(Artifact{SchemaVersion: SchemaVersion})
	if err != nil {
		t.Fatalf("render disabled audit: %v", err)
	}
	if !strings.Contains(html, `class="status not-reported"`) || strings.Contains(html, "This audit is partial") {
		t.Fatalf("disabled audit was not rendered as not reported:\n%s", html)
	}
}

func TestReadRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"2"}`), 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}
	if _, err := Read(path); err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("Read() error = %v, want schema version error", err)
	}
}

func TestBuildLeavesArtifactWhenReportWriteFails(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "audit.json")
	contents, err := json.Marshal(Artifact{SchemaVersion: SchemaVersion, Capture: Capture{Enabled: true, Status: "complete", Complete: true}})
	if err != nil {
		t.Fatalf("marshal audit fixture: %v", err)
	}
	if err := os.WriteFile(auditPath, contents, 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}
	outputPath := filepath.Join(directory, "existing-directory")
	if err := os.Mkdir(outputPath, 0o755); err != nil {
		t.Fatalf("create report directory fixture: %v", err)
	}
	if err := Build(auditPath, outputPath); err == nil {
		t.Fatal("Build() succeeded when report output was a directory")
	}
	if _, err := Read(auditPath); err != nil {
		t.Fatalf("artifact was damaged by report failure: %v", err)
	}
}
