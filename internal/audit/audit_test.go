package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestRenderShowsChronologicalFileModificationHistories(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-history"},
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		FileModifications: []FileModification{
			{
				Sequence:           1,
				ID:                 "file-modification-1",
				Path:               "api.py",
				Operation:          "execute_model_tool_call",
				ModelToolCallID:    "model-tool-call-1",
				AdapterOperationID: "adapter-operation-1",
				TurnID:             "iteration-1-turn-1",
				IterationID:        "iteration-1",
				Iteration:          1,
				Before:             json.RawMessage(`"one\n"`),
				After:              json.RawMessage(`"two\n"`),
				Diff:               "--- a/api.py\n+++ b/api.py\n-one\n+two\n",
			},
			{
				Sequence:    2,
				ID:          "file-modification-2",
				Path:        "api.py",
				TurnID:      "iteration-1-turn-2",
				IterationID: "iteration-1",
				Iteration:   1,
				Before:      json.RawMessage(`"two\n"`),
				After:       json.RawMessage(`"one\n"`),
				Diff:        "--- a/api.py\n+++ b/api.py\n-two\n+one\n",
			},
			{
				Sequence:    3,
				ID:          "file-modification-3",
				Path:        "new.py",
				TurnID:      "iteration-2-turn-1",
				IterationID: "iteration-2",
				Iteration:   2,
				Before:      json.RawMessage(`null`),
				After:       json.RawMessage(`"created\n"`),
				Diff:        "--- /dev/null\n+++ b/new.py\n+created\n",
				Created:     true,
			},
		},
	})
	if err != nil {
		t.Fatalf("render modification history: %v", err)
	}
	for _, fragment := range []string{
		"File modification history",
		"api.py",
		"new.py",
		"File Modification 1",
		"File Modification 2",
		"--- a/api.py",
		"-one",
		"&#43;two",
		"File did not exist",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("audit HTML missing %q:\n%s", fragment, html)
		}
	}
	if strings.Index(html, "File Modification 1") > strings.Index(html, "File Modification 2") {
		t.Fatal("file history does not preserve chronological order")
	}
}

func TestBuildFinalSourceUsesSnapshotsAndKeepsRestoredHistoryOutOfNetCount(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files: []SourceFile{
			{Path: "restored.py", Content: "original\n"},
			{Path: "deleted.py", Content: "remove me\n"},
		},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files: []SourceFile{
			{Path: "restored.py", Content: "original\n"},
			{Path: "created.py", Content: "new file\n"},
		},
	}

	result := BuildFinalSource(starting, final, nil, []FileModification{
		{Path: "restored.py", After: json.RawMessage(`"changed\n"`)},
		{Path: "restored.py", After: json.RawMessage(`"original\n"`)},
	})

	if result.Status != SourceStatusComplete || result.FilesChangedAtEnd != 2 {
		t.Fatalf("final source = %#v, want complete with two net files", result)
	}
	if len(result.Diffs) != 2 || result.Diffs[0].Path != "created.py" || result.Diffs[1].Path != "deleted.py" {
		t.Fatalf("final diffs = %#v, want created and deleted files only", result.Diffs)
	}
	if result.Diffs[0].Origin != ChangeOriginUnattributed {
		t.Fatalf("created file origin = %q, want unattributed", result.Diffs[0].Origin)
	}
}

func TestBuildFinalSourceAttributesObservedModelContent(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "before\n"}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "model result\n"}},
	}
	result := BuildFinalSource(starting, final, nil, []FileModification{
		{Path: "api.py", Before: json.RawMessage(`"before\n"`), After: json.RawMessage(`"model result\n"`)},
	})

	if len(result.Diffs) != 1 || result.Diffs[0].Origin != ChangeOriginModel {
		t.Fatalf("final diff = %#v, want one model-attributed change", result.Diffs)
	}
}

func TestBuildFinalSourceAttributesModelAndLaterLifecycleChange(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "before\n"}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "lifecycle result\n"}},
	}
	modelContent := "model result\n"
	lifecycleContent := "lifecycle result\n"

	result := BuildFinalSource(starting, final, []SourceChange{{
		Path: "api.py", Before: &modelContent, After: &lifecycleContent,
	}}, []FileModification{{
		Path:   "api.py",
		Before: json.RawMessage(`"before\n"`),
		After:  json.RawMessage(`"model result\n"`),
	}})

	if len(result.Diffs) != 1 || result.Diffs[0].Origin != ChangeOriginModelAndLifecycle {
		t.Fatalf("final diff = %#v, want model-and-lifecycle attribution", result.Diffs)
	}
}

func TestBuildFinalSourceAttributesASecondModelEdit(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "before\n"}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "second model result\n"}},
	}

	result := BuildFinalSource(starting, final, nil, []FileModification{
		{Path: "api.py", Before: json.RawMessage(`"before\n"`), After: json.RawMessage(`"first model result\n"`)},
		{Path: "api.py", Before: json.RawMessage(`"first model result\n"`), After: json.RawMessage(`"second model result\n"`)},
	})

	if len(result.Diffs) != 1 || result.Diffs[0].Origin != ChangeOriginModel {
		t.Fatalf("final diff = %#v, want model attribution through the second edit", result.Diffs)
	}
}

func TestBuildFinalSourceDoesNotAttributeUnconnectedModelHistory(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "before\n"}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: "final\n"}},
	}

	result := BuildFinalSource(starting, final, nil, []FileModification{{
		Path:   "api.py",
		Before: json.RawMessage(`"unrelated\n"`),
		After:  json.RawMessage(`"final\n"`),
	}})

	if len(result.Diffs) != 1 || result.Diffs[0].Origin != ChangeOriginUnattributed {
		t.Fatalf("final diff = %#v, want unattributed history", result.Diffs)
	}
}

func TestBuildFinalSourceMarksMissingSnapshotStatusUnavailable(t *testing.T) {
	result := BuildFinalSource(SourceSnapshot{}, SourceSnapshot{Status: SourceStatusComplete}, nil, nil)
	if result.Status != SourceStatusUnavailable {
		t.Fatalf("final source status = %q, want unavailable", result.Status)
	}
}

func TestCaptureSourceIncludesUntrackedFilesAndExcludesManagedState(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, ".git"), 0o755); err != nil {
		t.Fatalf("create git state: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(directory, ".local", "stbench"), 0o755); err != nil {
		t.Fatalf("create managed state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "untracked.py"), []byte("source\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".git", "HEAD"), []byte("head\n"), 0o644); err != nil {
		t.Fatalf("write git state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".local", "stbench", "stop.sh"), []byte("managed\n"), 0o644); err != nil {
		t.Fatalf("write managed state: %v", err)
	}

	snapshot := CaptureSource(directory, nil)
	if snapshot.Status != SourceStatusComplete || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "untracked.py" {
		t.Fatalf("source snapshot = %#v, want only untracked source", snapshot)
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

func TestAppendEvidencePreservesModelHistoryAndStoresRunnerEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark-audit.json")
	contents, err := json.Marshal(Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "in_progress"},
		FileModifications: []FileModification{{
			ID: "file-modification-1", Path: "api.py", Diff: "model diff",
		}},
	})
	if err != nil {
		t.Fatalf("marshal audit fixture: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}

	evidence := Evidence{
		FinalSource:        FinalSource{Status: SourceStatusUnavailable},
		LifecycleChanges:   []SourceChange{{ID: "lifecycle-change-1", Origin: ChangeOriginLifecycle}},
		ComparisonOutcomes: []ComparisonOutcome{{ID: "comparison-1", Status: "completed"}},
		EditSequences:      []EditSequence{{ID: "edit-sequence-1", EvaluationStatus: "not_evaluated"}},
	}
	if err := AppendEvidence(path, evidence); err != nil {
		t.Fatalf("append evidence: %v", err)
	}
	updated, err := Read(path)
	if err != nil {
		t.Fatalf("read updated audit: %v", err)
	}
	if len(updated.FileModifications) != 1 || len(updated.LifecycleChanges) != 1 ||
		len(updated.ComparisonOutcomes) != 1 || len(updated.EditSequences) != 1 ||
		updated.FinalSource.Status != SourceStatusUnavailable {
		t.Fatalf("updated audit = %#v, want existing model history plus runner evidence", updated)
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

func TestSummarizeEfficiencyKeepsKnownTokenSubtotalPartialAndIncludesFailedTiming(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture: Capture{
			Enabled:             true,
			Status:              "complete",
			Complete:            true,
			RecordingOverheadMS: 12,
		},
		Iterations: []Iteration{
			{ID: "iteration-1", Number: 1},
			{ID: "iteration-2", Number: 2},
		},
		Events: []Event{
			{
				Type: "model_turn", IterationID: "iteration-1", Status: "completed",
				DurationMS: 31, EndedAt: "2026-01-01T00:00:01Z", RecordingOverheadMS: 5,
				Tokens: &benchrecord.TokenUsage{Input: 4, Output: 2, Total: 6},
			},
			{
				Type: "model_turn", IterationID: "iteration-1", Status: "failed",
				DurationMS: 7, EndedAt: "2026-01-01T00:00:02Z", RecordingOverheadMS: 3,
			},
			{
				Type: "model_turn", IterationID: "iteration-2", Status: "completed",
				DurationMS: 11, EndedAt: "2026-01-01T00:00:03Z", RecordingOverheadMS: 4,
				Tokens: &benchrecord.TokenUsage{Input: 5, Output: 1, Total: 6},
			},
		},
	}

	summary := SummarizeEfficiency(document)
	if summary.Status != benchrecord.EfficiencyStatusPartial || summary.Turns != 3 ||
		summary.CompletedTurns != 2 || summary.FailedTurns != 1 {
		t.Fatalf("run efficiency = %#v, want complete three-turn evidence", summary)
	}
	if summary.TokenStatus != benchrecord.TokenStatusPartial || summary.KnownTokenTurns != 2 ||
		summary.UnknownTokenTurns != 1 || summary.Tokens == nil {
		t.Fatalf("token evidence = %#v, want partial known subtotal", summary)
	}
	if *summary.Tokens != (benchrecord.TokenUsage{Input: 9, Output: 3, Total: 12}) {
		t.Fatalf("token subtotal = %#v, want input 9/output 3/total 12", summary.Tokens)
	}
	if summary.InferenceMS != 49 || summary.MeasuredInferenceTurns != 3 || summary.RecordingOverheadMS != 12 {
		t.Fatalf("timing evidence = %#v, want inference 49ms and overhead 12ms", summary)
	}

	iteration := summarizeIterationEfficiency(document, "iteration-1")
	if iteration.InferenceMS != 38 || iteration.RecordingOverheadMS != 8 ||
		iteration.TokenStatus != benchrecord.TokenStatusPartial {
		t.Fatalf("iteration efficiency = %#v, want iteration-1 breakdown", iteration)
	}
}

func TestSummarizeEfficiencyKeepsZeroRunOverheadDistinctFromIterationOverhead(t *testing.T) {
	document := Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Events: []Event{{
			Type: "model_turn", IterationID: "iteration-1", Status: "completed",
			EndedAt: "2026-01-01T00:00:01Z", RecordingOverheadMS: 4,
		}},
	}

	if summary := SummarizeEfficiency(document); summary.RecordingOverheadMS != 0 {
		t.Fatalf("run overhead = %d, want the recorded zero", summary.RecordingOverheadMS)
	}
	if summary := summarizeIterationEfficiency(document, "iteration-1"); summary.RecordingOverheadMS != 4 {
		t.Fatalf("iteration overhead = %d, want event overhead", summary.RecordingOverheadMS)
	}
}

func TestEventOmitsUnmeasuredDuration(t *testing.T) {
	data, err := json.Marshal(Event{Type: "model_turn", Status: "started"})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(data), `"duration_ms"`) {
		t.Fatalf("event = %s, want duration omitted when it is unmeasured", data)
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
