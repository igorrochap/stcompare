package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stcompare/benchrecord"
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

func TestRenderShowsFinalSourceLifecycleAndComparisonEvidence(t *testing.T) {
	before := "before\n"
	after := "after\n"
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-evidence"},
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		FinalSource: FinalSource{
			Status:            SourceStatusComplete,
			Starting:          SourceSnapshot{Status: SourceStatusComplete},
			Final:             SourceSnapshot{Status: SourceStatusComplete},
			FilesChangedAtEnd: 1,
			Diffs: []SourceChange{{
				ID: "final-source-diff-1", Path: "api.py", Origin: ChangeOriginUnattributed,
				Before: &before, After: &after, Diff: "--- a/api.py\n+++ b/api.py\n",
			}},
		},
		LifecycleChanges: []SourceChange{{
			Sequence: 1, ID: "lifecycle-change-1", Path: "generated.py", Phase: "build",
			Origin: ChangeOriginLifecycle, Diff: "build diff",
		}},
		ComparisonOutcomes: []ComparisonOutcome{{
			Sequence: 1, ID: "comparison-1", Iteration: 1, Status: "completed", ExitCode: 2,
			View: json.RawMessage(`{"counts":{"still_failing":1}}`),
		}},
		EditSequences: []EditSequence{{
			Sequence: 1, ID: "edit-sequence-1", Iteration: 1,
			ProblemInput:       "Problems delivered to the model\n{\"actionable\":[{\"id\":\"problem-1\"}]}\n",
			ComparisonBeforeID: "comparison-1", EvaluationStatus: "not_evaluated",
		}},
	})
	if err != nil {
		t.Fatalf("render evidence audit: %v", err)
	}
	for _, fragment := range []string{
		"Final source", "Files Changed at the End", "unattributed", "Lifecycle changes",
		"excluded from model edit counts", "Problems delivered to model", "problem-1",
		"not evaluated", "Fixed is a replay-backed Problem Outcome", "Fix Quality Assessment",
		"Git HEAD is not used",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("evidence audit HTML missing %q:\n%s", fragment, html)
		}
	}
}

func TestRenderShowsEfficiencySummaryAndUnknownTurnLabel(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-efficiency"},
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true, RecordingOverheadMS: 19},
		Events: []Event{
			{
				Type: "model_turn", TurnID: "turn-1", Status: "completed", EndedAt: "done",
				DurationMS: 42, Tokens: &benchrecord.TokenUsage{Input: 8, Output: 3, Total: 11},
			},
			{Type: "model_turn", TurnID: "turn-2", Status: "failed", EndedAt: "failed", DurationMS: 6},
		},
	})
	if err != nil {
		t.Fatalf("render efficiency audit: %v", err)
	}
	for _, fragment := range []string{
		"Efficiency summary",
		"Inference time",
		"48 ms",
		"Audit-recording overhead",
		"19 ms",
		"partial",
		"Server-reported tokens: 8 input · 3 output · 11 total.",
		"Token usage</span><strong>unknown</strong>",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("efficiency audit missing %q:\n%s", fragment, html)
		}
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
		Iterations:    []Iteration{{ID: "iteration-1", Number: 1}},
		Events: []Event{{
			Type:        "model_turn",
			IterationID: "iteration-1",
			Status:      "completed",
			Input:       json.RawMessage(`{"messages":[{"role":"user","content":"saved evidence"}]}`),
		}},
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
	rendered, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read audit report: %v", err)
	}
	if !strings.Contains(string(rendered), `<summary>Exact model input (JSON)</summary>`) ||
		strings.Contains(string(rendered), `<details class="payload" open>`) {
		t.Fatalf("saved audit report did not render its payload collapsed:\n%s", rendered)
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
