package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildFinalSourceShowsFocusedContextForSmallEdit(t *testing.T) {
	startingContent := strings.Join([]string{
		"line 1", "line 2", "line 3", "line 4", "line 5", "line 6",
		"line 7", "line 8", "line 9", "old line", "line 11", "line 12",
		"line 13", "line 14", "line 15", "line 16", "line 17", "line 18",
	}, "\n") + "\n"
	finalContent := strings.Replace(startingContent, "old line", "new line", 1)
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: startingContent}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: finalContent}},
	}

	result := BuildFinalSource(starting, final, nil, nil)
	if len(result.Diffs) != 1 {
		t.Fatalf("final diffs = %#v, want one changed file", result.Diffs)
	}
	if got, want := result.Diffs[0].Diff, "--- a/api.py\n+++ b/api.py\n@@ -7,7 +7,7 @@\n line 7\n line 8\n line 9\n-old line\n+new line\n line 11\n line 12\n line 13\n"; got != want {
		t.Fatalf("focused diff = %q, want %q", got, want)
	}
}

func TestBuildFinalSourceSeparatesDistantFocusedDiffHunks(t *testing.T) {
	startingLines := make([]string, 24)
	for index := range startingLines {
		startingLines[index] = fmt.Sprintf("line %d", index+1)
	}
	startingContent := strings.Join(startingLines, "\n") + "\n"
	finalContent := strings.Replace(strings.Replace(startingContent, "line 2", "first change", 1), "line 20", "second change", 1)
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: startingContent}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "api.py", Content: finalContent}},
	}

	result := BuildFinalSource(starting, final, nil, nil)
	if len(result.Diffs) != 1 {
		t.Fatalf("final diffs = %#v, want one changed file", result.Diffs)
	}
	diff := result.Diffs[0].Diff
	for _, hunk := range []string{"@@ -1,5 +1,5 @@", "@@ -17,7 +17,7 @@"} {
		if !strings.Contains(diff, hunk) {
			t.Fatalf("focused diff = %q, want hunk %q", diff, hunk)
		}
	}
	if strings.Count(diff, "@@ ") != 2 {
		t.Fatalf("focused diff = %q, want two separate hunks", diff)
	}
}

func TestBuildFinalSourcePreservesFileExistenceAndMissingFinalNewline(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "empty.py", Content: ""}, {Path: "newline.py", Content: "line\n"}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "empty.py", Content: ""}, {Path: "newline.py", Content: "line"}, {Path: "created.py", Content: ""}},
	}

	result := BuildFinalSource(starting, final, nil, nil)
	if len(result.Diffs) != 2 {
		t.Fatalf("final diffs = %#v, want newline change and empty creation", result.Diffs)
	}
	newlineDiff := result.Diffs[1].Diff
	if !strings.Contains(newlineDiff, "\\ No newline at end of file") {
		t.Fatalf("newline diff = %q, want missing-final-newline marker", newlineDiff)
	}
	created := result.Diffs[0]
	if !created.Created || created.Before != nil || created.After == nil || *created.After != "" {
		t.Fatalf("empty creation = %#v, want absent before and exact empty after", created)
	}
	if !strings.Contains(created.Diff, "--- /dev/null\n+++ b/created.py") {
		t.Fatalf("empty creation diff = %q, want absent-file header", created.Diff)
	}
}

func TestBuildFinalSourceKeepsAllLinesForCreationAndDeletion(t *testing.T) {
	starting := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "deleted.py", Content: "remove 1\nremove 2\nremove 3\n"}},
	}
	final := SourceSnapshot{
		Status: SourceStatusComplete,
		Files:  []SourceFile{{Path: "created.py", Content: "add 1\nadd 2\nadd 3\n"}},
	}

	result := BuildFinalSource(starting, final, nil, nil)
	if len(result.Diffs) != 2 {
		t.Fatalf("final diffs = %#v, want creation and deletion", result.Diffs)
	}
	created, deleted := result.Diffs[0], result.Diffs[1]
	if !created.Created || !strings.Contains(created.Diff, "+add 1\n+add 2\n+add 3\n") {
		t.Fatalf("creation diff = %q, want every added line", created.Diff)
	}
	if !deleted.Deleted || !strings.Contains(deleted.Diff, "-remove 1\n-remove 2\n-remove 3\n") {
		t.Fatalf("deletion diff = %q, want every removed line", deleted.Diff)
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
