package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderCollapsesJSONPayloadsWithIndependentDisclosureControls(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Run:           Run{ID: "run-payloads"},
		Capture:       Capture{Enabled: true, Status: "partial", Complete: false},
		Iterations:    []Iteration{{ID: "iteration-1", Number: 1}},
		Events: []Event{
			{
				Sequence:    1,
				Type:        "model_turn",
				Iteration:   1,
				IterationID: "iteration-1",
				TurnID:      "turn-1",
				Status:      "failed",
				StartedAt:   "started",
				EndedAt:     "ended",
				DurationMS:  42,
				Input:       json.RawMessage(`{"messages":[{"role":"user","content":"<payload>"}]}`),
				Sampling:    map[string]any{"temperature": 0.2},
				Returned:    json.RawMessage(`{"error":"model unavailable"}`),
				ReturnedMessages: []json.RawMessage{
					json.RawMessage(`{"role":"assistant","content":"first rationale"}`),
					json.RawMessage(`{"role":"tool","content":"second message"}`),
				},
				Error: "model unavailable",
			},
			{
				Sequence:    2,
				Type:        "model_tool_call",
				Iteration:   1,
				IterationID: "iteration-1",
				TurnID:      "turn-1",
				ID:          "tool-1",
				ToolName:    "read_file",
				Status:      "completed",
				Arguments:   json.RawMessage(`{"path":"api.py"}`),
				Request:     json.RawMessage(`{"name":"read_file"}`),
				Result:      json.RawMessage(`{"content":"<source>"}`),
			},
			{
				Sequence:    3,
				Type:        "adapter_operation",
				Iteration:   1,
				IterationID: "iteration-1",
				TurnID:      "turn-1",
				ID:          "operation-1",
				Operation:   "validate_patch",
				Status:      "completed",
				Arguments:   json.RawMessage(`{"patch":"diff"}`),
				Result:      json.RawMessage(`{"valid":true}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("render audit payloads: %v", err)
	}

	for _, label := range []string{
		"Exact model input (JSON)",
		"Effective sampling settings (JSON)",
		"Returned model response (JSON)",
		"Returned model message 1 (JSON)",
		"Returned model message 2 (JSON)",
		"Tool arguments (JSON)",
		"Tool request (JSON)",
		"Tool result (JSON)",
		"Adapter Operation arguments (JSON)",
		"Adapter Operation result (JSON)",
	} {
		if !strings.Contains(html, "<summary>"+label+"</summary>") {
			t.Fatalf("audit HTML missing JSON disclosure label %q:\n%s", label, html)
		}
	}
	if got := strings.Count(html, `<details class="payload">`); got != 10 {
		t.Fatalf("JSON disclosure count = %d, want one control per payload", got)
	}
	if strings.Contains(html, `<details class="payload" open>`) {
		t.Fatalf("JSON disclosure is open by default:\n%s", html)
	}
	for _, fragment := range []string{
		"turn-1", "failed", "started", "ended", "42 ms", "model unavailable",
		"first rationale", "&lt;payload&gt;", "&lt;source&gt;",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("audit HTML missing visible or escaped evidence %q:\n%s", fragment, html)
		}
	}
}

func TestRenderShowsPlaceholdersForMissingCorePayloadsAndOmitsOptionalPayloads(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		Iterations:    []Iteration{{ID: "iteration-1", Number: 1}},
		Events: []Event{
			{
				Type:             "model_turn",
				IterationID:      "iteration-1",
				Status:           "completed",
				Input:            json.RawMessage(`null`),
				Returned:         json.RawMessage(`null`),
				ReturnedMessages: []json.RawMessage{json.RawMessage(`null`)},
			},
			{
				Type:        "model_tool_call",
				IterationID: "iteration-1",
				Status:      "completed",
				ID:          "tool-1",
				Arguments:   json.RawMessage(`null`),
				Request:     json.RawMessage(`null`),
				Result:      json.RawMessage(`null`),
			},
			{
				Type:        "adapter_operation",
				IterationID: "iteration-1",
				Status:      "completed",
				ID:          "operation-1",
				Arguments:   json.RawMessage(`null`),
				Result:      json.RawMessage(`null`),
			},
		},
	})
	if err != nil {
		t.Fatalf("render audit without optional payloads: %v", err)
	}
	for _, label := range []string{
		"Effective sampling settings (JSON)",
		"Returned model response (JSON)",
		"Tool arguments (JSON)",
		"Tool request (JSON)",
		"Tool result (JSON)",
		"Adapter Operation arguments (JSON)",
		"Adapter Operation result (JSON)",
	} {
		if strings.Contains(html, label) {
			t.Fatalf("audit HTML showed missing JSON disclosure %q:\n%s", label, html)
		}
	}
	for _, label := range []string{
		"Exact model input (JSON)",
		"Returned model message 1 (JSON)",
	} {
		if !strings.Contains(html, "<summary>"+label+"</summary>") {
			t.Fatalf("audit HTML omitted missing core payload %q:\n%s", label, html)
		}
	}
	if got := strings.Count(html, "not captured"); got != 2 {
		t.Fatalf("missing core payload placeholders = %d, want two:\n%s", got, html)
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

func TestRenderFocusesFileModificationHistoryForCreatedAndDeletedFiles(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		FileModifications: []FileModification{
			{
				Sequence: 1,
				Path:     "created.py",
				Before:   nil,
				After:    json.RawMessage(`"created 1\ncreated 2\n"`),
				Diff:     "captured created diff",
				Created:  true,
			},
			{
				Sequence: 2,
				Path:     "deleted.py",
				Before:   json.RawMessage(`"deleted 1\ndeleted 2\n"`),
				After:    nil,
				Diff:     "captured deleted diff",
			},
		},
	})
	if err != nil {
		t.Fatalf("render created and deleted file history: %v", err)
	}
	for _, capturedDiff := range []string{"captured created diff", "captured deleted diff"} {
		if strings.Contains(html, capturedDiff) {
			t.Fatalf("rendered file history used stored diff %q", capturedDiff)
		}
	}
	for _, fragment := range []string{
		"--- /dev/null", "&#43;&#43;&#43; b/created.py", "&#43;created 1", "&#43;created 2",
		"--- a/deleted.py", "&#43;&#43;&#43; /dev/null", "-deleted 1", "-deleted 2",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("created and deleted file history HTML missing %q:\n%s", fragment, html)
		}
	}
}

func TestRenderPresentsFocusedDiffBeforeCollapsedExactSourceViews(t *testing.T) {
	before := "line 1\nold line\nline 3\n"
	after := "line 1\nnew line\nline 3\n"
	historyBefore := json.RawMessage(`"line 1\nold line\nline 3\n"`)
	historyAfter := json.RawMessage(`"line 1\nnew line\nline 3\n"`)
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "complete", Complete: true},
		FinalSource: FinalSource{
			Status: SourceStatusComplete,
			Diffs: []SourceChange{{
				Path: "final.py", Before: &before, After: &after,
				Diff: "captured whole-file diff",
			}},
		},
		LifecycleChanges: []SourceChange{{
			Sequence: 1, Path: "lifecycle.py", Phase: "build",
			Before: &before, After: &after, Diff: "captured lifecycle diff",
		}},
		FileModifications: []FileModification{{
			Sequence: 1, Path: "history.py", Before: historyBefore, After: historyAfter,
			Diff: "captured history diff",
		}},
	})
	if err != nil {
		t.Fatalf("render focused diff audit: %v", err)
	}
	for _, fragment := range []string{
		"Focused diff", "Before (full source)", "After (full source)",
		"-old line", "&#43;new line", "line 1", "old line", "new line", "line 3",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("focused diff audit HTML missing %q:\n%s", fragment, html)
		}
	}
	for _, capturedDiff := range []string{"captured whole-file diff", "captured lifecycle diff", "captured history diff"} {
		if strings.Contains(html, capturedDiff) {
			t.Fatalf("rendered audit used stored unfocused diff %q", capturedDiff)
		}
	}
	if strings.Contains(html, "<details open") {
		t.Fatalf("rendered source entries are not collapsed by default:\n%s", html)
	}
	if strings.Index(html, "Focused diff") > strings.Index(html, "Before (full source)") ||
		strings.Index(html, "Before (full source)") > strings.Index(html, "After (full source)") {
		t.Fatalf("rendered source evidence is not ordered focused diff, before, after")
	}
}

func TestRenderRetainsCapturedDiffWhenSourceEvidenceIsIncomplete(t *testing.T) {
	html, err := Render(Artifact{
		SchemaVersion: SchemaVersion,
		Capture:       Capture{Enabled: true, Status: "partial", Complete: false},
		FinalSource: FinalSource{
			Status:            SourceStatusPartial,
			Starting:          SourceSnapshot{Status: SourceStatusPartial},
			Final:             SourceSnapshot{Status: SourceStatusPartial},
			FilesChangedAtEnd: 1,
			Diffs:             []SourceChange{{Path: "partial.py", Diff: "captured partial diff"}},
		},
		LifecycleChanges: []SourceChange{{Path: "unknown.py", Diff: "captured unavailable diff"}},
	})
	if err != nil {
		t.Fatalf("render incomplete source audit: %v", err)
	}
	for _, fragment := range []string{
		"This audit is partial", "Final source snapshot is partial", "starting: partial · final: partial",
		"captured partial diff", "captured unavailable diff",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("incomplete source audit HTML missing %q:\n%s", fragment, html)
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
