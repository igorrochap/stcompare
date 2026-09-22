package bench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
	"stcompare/internal/audit"
)

func TestRunConvergesOnFirstIteration(t *testing.T) {
	view := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Converged:     true,
		Candidate:     "candidate",
		Baseline:      "baseline",
		Counts:        agentreport.Counts{Fixed: 2},
	}
	comparator := &fakeComparator{results: []comparisonResult{{view: view, exitCode: agentreport.ExitCodeConverged}}}
	candidate := &fakeCandidate{}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		AdapterMetadata: AdapterMetadata{Agent: "agent"},
		Candidate:       "candidate",
		Baseline:        "baseline",
		BaselineExists:  func() bool { return true },
		MaxIterations:   3,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if record.Iterations != 1 {
		t.Fatalf("iterations = %d, want 1", record.Iterations)
	}
	if record.Temperature != 0 {
		t.Fatalf("record temperature = %v, want deterministic default 0", record.Temperature)
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter calls = %d, want 0", len(adapter.instructions))
	}
	if len(record.RemainingActionable) != 0 {
		t.Fatalf("remaining actionable = %#v, want empty", record.RemainingActionable)
	}
	if got, want := candidate.calls, preflightAndFirstIterationCalls; !sameStrings(got, want) {
		t.Fatalf("candidate calls = %#v, want %#v", got, want)
	}
	if len(comparator.configs) != 1 {
		t.Fatalf("comparator calls = %d, want 1", len(comparator.configs))
	}
}

func TestRunPassesStableAuditContextToEachAdapterBoundary(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	comparator := &fakeComparator{results: []comparisonResult{
		{view: agentreport.View{Actionable: []agentreport.Actionable{{ID: "problem-1"}}}, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		RunID:           "run-fixed",
		Candidate:       "candidate",
		Baseline:        "baseline",
		AuditPath:       auditPath,
		AuditReportPath: filepath.Join(directory, "benchmark-audit.html"),
		BaselineExists:  func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(adapter.preflightMetadata) != 1 || adapter.preflightMetadata[0].Audit == nil {
		t.Fatalf("preflight audit context = %#v, want one context", adapter.preflightMetadata)
	}
	if len(adapter.metadata) != 1 || adapter.metadata[0].Audit == nil {
		t.Fatalf("fix audit context = %#v, want one context", adapter.metadata)
	}
	preflight := adapter.preflightMetadata[0].Audit
	fix := adapter.metadata[0].Audit
	if preflight.RunID != "run-fixed" || fix.RunID != "run-fixed" ||
		preflight.IterationID != "preflight" || fix.IterationID != "iteration-1" ||
		!preflight.Enabled || fix.Path != auditPath {
		t.Fatalf("audit contexts = %#v and %#v, want stable run and iteration identities", preflight, fix)
	}
	if record.RunID != "run-fixed" || record.Audit.Status != benchrecord.AuditStatusNotReported {
		t.Fatalf("record audit identity/status = %#v, want run-fixed and not_reported for unsupported fake", record.Audit)
	}
}

func TestRunAuditCapturesLifecycleChangesAndComparisonSequences(t *testing.T) {
	directory := t.TempDir()
	sourceDir := filepath.Join(directory, "source")
	if err := os.Mkdir(sourceDir, 0o755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	candidate := &sourceLifecycleCandidate{
		fakeCandidate: &fakeCandidate{},
		path:          filepath.Join(sourceDir, "generated.py"),
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: agentreport.View{
			Counts:     agentreport.Counts{StillFailing: 1},
			Actionable: []agentreport.Actionable{{ID: "problem-1"}},
		}, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true, Counts: agentreport.Counts{Fixed: 1}}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &artifactAdapter{fakeAdapter: &fakeAdapter{}}

	_, err := Run(Config{
		RunID:          "run-source-evidence",
		Candidate:      "candidate",
		Baseline:       "baseline",
		SourceDir:      sourceDir,
		AuditPath:      auditPath,
		MaxIterations:  2,
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	document, err := audit.Read(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if document.FinalSource.Status != audit.SourceStatusComplete || document.FinalSource.FilesChangedAtEnd != 1 {
		t.Fatalf("final source = %#v, want one complete net change", document.FinalSource)
	}
	if got := document.FinalSource.Diffs[0].Origin; got != audit.ChangeOriginLifecycle {
		t.Fatalf("final diff origin = %q, want lifecycle", got)
	}
	if len(document.LifecycleChanges) == 0 || len(document.ComparisonOutcomes) != 2 || len(document.EditSequences) != 1 {
		t.Fatalf("audit evidence counts = lifecycle %d, comparisons %d, edit sequences %d", len(document.LifecycleChanges), len(document.ComparisonOutcomes), len(document.EditSequences))
	}
	if document.Activity.FileModifications != 0 || len(document.FileModifications) != 0 {
		t.Fatalf("lifecycle changes inflated model edit counts: activity=%#v modifications=%#v", document.Activity, document.FileModifications)
	}
	sequence := document.EditSequences[0]
	if sequence.ComparisonBeforeID != "comparison-1" || sequence.SubsequentComparisonID != "comparison-2" ||
		sequence.EvaluationStatus != "evaluated" || sequence.ProblemInput != adapter.instructions[0] {
		t.Fatalf("edit sequence = %#v, want chronological problem and outcome evidence", sequence)
	}
}

func TestRunAuditMarksEditWithoutSubsequentComparisonAsNotEvaluated(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Counts: agentreport.Counts{StillFailing: 1}, Actionable: []agentreport.Actionable{{ID: "problem-1"}}},
		exitCode: agentreport.ExitCodeNotConverged,
	}}}
	adapter := &artifactAdapter{
		fakeAdapter: &fakeAdapter{errs: []error{errors.New("edit stopped")}},
	}

	_, err := Run(Config{
		RunID:          "run-not-evaluated",
		AuditPath:      auditPath,
		MaxIterations:  2,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err == nil {
		t.Fatal("Run() succeeded, want adapter error")
	}

	document, err := audit.Read(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(document.EditSequences) != 1 || document.EditSequences[0].EvaluationStatus != "not_evaluated" ||
		document.EditSequences[0].SubsequentComparisonID != "" {
		t.Fatalf("edit sequence = %#v, want no subsequent comparison", document.EditSequences)
	}
}

func TestSourceTrackerExcludesBenchmarkReportDirectory(t *testing.T) {
	directory := t.TempDir()
	sourceDir := filepath.Join(directory, "source")
	reportsDir := filepath.Join(sourceDir, "reports")
	reportDir := filepath.Join(reportsDir, "candidate")
	otherReportDir := filepath.Join(reportsDir, "other-candidate")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("create source/report directories: %v", err)
	}
	if err := os.MkdirAll(otherReportDir, 0o755); err != nil {
		t.Fatalf("create other report directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "api.py"), []byte("source\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "comparison.json"), []byte("generated\n"), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherReportDir, "benchmark-audit.json"), []byte("previous audit\n"), 0o644); err != nil {
		t.Fatalf("write other audit: %v", err)
	}

	tracker := newSourceTracker(Config{
		SourceDir:       sourceDir,
		ReportsDir:      reportsDir,
		AuditPath:       filepath.Join(reportDir, "benchmark-audit.json"),
		AuditReportPath: filepath.Join(reportDir, "benchmark-audit.html"),
	})
	tracker.captureStarting()
	final := tracker.finalSource()
	for name, snapshot := range map[string]audit.SourceSnapshot{
		"starting": tracker.starting,
		"final":    final.Final,
	} {
		if snapshot.Status != audit.SourceStatusComplete || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "api.py" {
			t.Fatalf("%s source snapshot = %#v, want source without generated reports", name, snapshot)
		}
	}
}

func TestSourceExcludesKeepsExistingExclusionsWhenReportsOutsideSource(t *testing.T) {
	directory := t.TempDir()
	sourceDir := filepath.Join(directory, "source")
	reportsDir := filepath.Join(directory, "reports")
	auditDir := filepath.Join(sourceDir, ".audit")
	auditPath := filepath.Join(auditDir, "benchmark-audit.json")
	auditReportPath := filepath.Join(auditDir, "benchmark-audit.html")

	got := sourceExcludes(Config{
		SourceDir:       sourceDir,
		ReportsDir:      reportsDir,
		AuditPath:       auditPath,
		AuditReportPath: auditReportPath,
	})
	want := []string{auditPath, auditReportPath, auditDir}
	if len(got) != len(want) {
		t.Fatalf("source exclusions = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("source exclusions = %#v, want %#v", got, want)
		}
	}
}

func TestRunMarksAuditCaptureFailureAsExplicitTerminalError(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Actionable: []agentreport.Actionable{{ID: "problem-1"}}},
		exitCode: agentreport.ExitCodeNotConverged,
	}}}
	adapter := &fakeAdapter{errs: []error{&AuditFailureError{Err: errors.New("disk full")}}}

	record, err := Run(Config{BaselineExists: func() bool { return true }}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err == nil || !strings.Contains(err.Error(), "audit capture failed") {
		t.Fatalf("Run() error = %v, want explicit audit failure", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAuditError {
		t.Fatalf("terminal state = %q, want audit_error", record.TerminalState)
	}
	if len(comparator.configs) != 1 || len(adapter.instructions) != 1 {
		t.Fatalf("work after audit failure: comparisons=%d fixes=%d", len(comparator.configs), len(adapter.instructions))
	}
}

func TestRunMarksAuditFailureWhenArtifactCouldNotBeCreated(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	adapter := &fakeAdapter{preflightErr: &AuditFailureError{Err: errors.New("permission denied")}}

	record, err := Run(Config{
		AuditPath:      auditPath,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: &fakeComparator{}, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Run() error = %v, want audit creation failure", err)
	}
	if record.Audit.Status != benchrecord.AuditStatusPartial || record.Audit.Error == "" {
		t.Fatalf("audit reference = %#v, want partial failure", record.Audit)
	}
}

func TestRunFinalizesAvailableAuditAfterSuccessfulRun(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	adapter := &artifactAdapter{fakeAdapter: &fakeAdapter{}}
	comparator := &fakeComparator{results: []comparisonResult{{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged}}}

	record, err := Run(Config{
		RunID:           "run-complete",
		Candidate:       "candidate",
		Baseline:        "baseline",
		AuditPath:       auditPath,
		AuditReportPath: filepath.Join(directory, "benchmark-audit.html"),
		BaselineExists:  func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.Audit.Status != benchrecord.AuditStatusComplete {
		t.Fatalf("audit status = %q, want complete", record.Audit.Status)
	}
	if record.Audit.Activity == nil || record.Audit.Activity.Status != benchrecord.ActivityStatusNotReported ||
		record.Audit.Activity.ModelToolCalls.Count != 0 {
		t.Fatalf("audit activity = %#v, want not-reported activity evidence", record.Audit.Activity)
	}
	if record.Efficiency.Turns != 1 || record.Efficiency.TokenStatus != benchrecord.TokenStatusUnknown ||
		record.Efficiency.MeasuredInferenceTurns != 1 {
		t.Fatalf("record efficiency = %#v, want one measured unknown-token turn", record.Efficiency)
	}
	if len(record.IterationEfficiency) != 1 || record.IterationEfficiency[0].Turns != 1 {
		t.Fatalf("iteration efficiency = %#v, want one aggregate with one turn", record.IterationEfficiency)
	}
	document, err := audit.Read(auditPath)
	if err != nil {
		t.Fatalf("read finalized audit: %v", err)
	}
	if document.Capture.Status != "complete" || document.Run.TerminalState != string(benchrecord.TerminalStateConverged) {
		t.Fatalf("finalized audit = %#v, want complete converged evidence", document)
	}
}

func TestRunKeepsAuditCompleteWhenRunFailsAfterCapturedTurns(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	adapter := &artifactAdapter{fakeAdapter: &fakeAdapter{}}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: agentreport.View{Actionable: []agentreport.Actionable{{ID: "problem-1"}}}, exitCode: agentreport.ExitCodeNotConverged},
		{err: errors.New("comparison failed")},
	}}

	record, err := Run(Config{
		RunID:          "run-error-after-capture",
		AuditPath:      auditPath,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err == nil || !strings.Contains(err.Error(), "comparison failed") {
		t.Fatalf("Run() error = %v, want comparison failure", err)
	}
	if record.Audit.Status != benchrecord.AuditStatusComplete {
		t.Fatalf("audit status = %q, want complete captured evidence", record.Audit.Status)
	}
	document, err := audit.Read(auditPath)
	if err != nil {
		t.Fatalf("read finalized audit: %v", err)
	}
	if document.Capture.Status != "complete" || !document.Capture.Complete {
		t.Fatalf("capture = %#v, want complete capture", document.Capture)
	}
}

func TestAuditCaptureTreatsTerminalToolFailuresAsCompleteEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark-audit.json")
	contents := []byte(`{
  "schema_version": "1",
  "capture": {"enabled": true, "status": "in_progress", "complete": false},
  "events": [
    {"type": "model_turn", "status": "completed"},
    {"type": "model_tool_call", "status": "failed"},
    {"type": "adapter_operation", "status": "failed"}
  ]
}`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}

	partial, err := auditCaptureIsPartial(path, nil)
	if err != nil {
		t.Fatalf("inspect audit capture: %v", err)
	}
	if partial {
		t.Fatal("terminal tool failures were treated as incomplete capture")
	}
}

func TestRunStopsWithAuditErrorWhenFinalizationCannotUpdateArtifact(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	adapter := &invalidArtifactAdapter{fakeAdapter: &fakeAdapter{}}
	comparator := &fakeComparator{results: []comparisonResult{{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged}}}

	record, err := Run(Config{
		RunID:          "run-invalid-audit",
		AuditPath:      auditPath,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err == nil || !strings.Contains(err.Error(), "audit capture failed") {
		t.Fatalf("Run() error = %v, want finalization audit failure", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAuditError || record.Audit.Status != benchrecord.AuditStatusPartial {
		t.Fatalf("record = %#v, want audit error and partial evidence", record)
	}
}

func TestRunClosesAdapterAndRecordsNegotiatedProcessReuse(t *testing.T) {
	adapter := &trackingAdapter{
		fakeAdapter:  &fakeAdapter{},
		processReuse: true,
	}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		ReuseProcess:   true,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if adapter.closeCalls != 1 {
		t.Fatalf("adapter close calls = %d, want 1", adapter.closeCalls)
	}
	if !record.ProcessReuse {
		t.Fatal("record.ProcessReuse = false, want true")
	}
}

func TestRunKeepsConvergedRecordWhenAdapterCloseFails(t *testing.T) {
	adapter := &trackingAdapter{
		fakeAdapter:  &fakeAdapter{},
		processReuse: true,
		closeErr:     errors.New("adapter process exited on cleanup"),
	}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		ReuseProcess:   true,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("Run() error = %v, want cleanup error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want converged", record.TerminalState)
	}
}

func TestRunRecordsColdFallbackWhenAdapterDoesNotNegotiateReuse(t *testing.T) {
	adapter := &trackingAdapter{fakeAdapter: &fakeAdapter{}, processReuse: false}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		ReuseProcess:   true,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.ProcessReuse {
		t.Fatal("record.ProcessReuse = true, want cold fallback")
	}
}

func TestRunPreflightsLifecycleAndAdapterBeforeComparison(t *testing.T) {
	view := agentreport.View{
		Converged: true,
		Counts:    agentreport.Counts{Fixed: 1},
	}
	comparator := &fakeComparator{results: []comparisonResult{{view: view, exitCode: agentreport.ExitCodeConverged}}}
	candidate := &fakeCandidate{}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  1,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if len(adapter.preflightMetadata) != 1 {
		t.Fatalf("adapter preflight calls = %d, want 1", len(adapter.preflightMetadata))
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter fix calls = %d, want 0 for a converged comparison", len(adapter.instructions))
	}
	if len(comparator.configs) != 1 {
		t.Fatalf("comparator calls = %d, want 1 after preflight", len(comparator.configs))
	}
	if got, want := candidate.calls, preflightAndFirstIterationCalls; !sameStrings(got, want) {
		t.Fatalf("candidate calls = %#v, want %#v", got, want)
	}
}

func TestRunReportsPreflightLifecycleFailureBeforeComparison(t *testing.T) {
	candidate := &fakeCandidate{failPhase: "build", failErr: errors.New("compile failed")}
	comparator := &fakeComparator{}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "preflight") || !strings.Contains(err.Error(), "build") {
		t.Fatalf("Run error = %v, want preflight build error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateLifecycleError || record.LifecyclePhase != benchrecord.LifecyclePhaseBuild {
		t.Fatalf("record = %#v, want preflight build lifecycle error", record)
	}
	if record.Iterations != 0 || len(comparator.configs) != 0 {
		t.Fatalf("preflight failure performed comparison work: iterations=%d comparisons=%d", record.Iterations, len(comparator.configs))
	}
}

func TestRunReportsPreflightAdapterFailureBeforeComparison(t *testing.T) {
	comparator := &fakeComparator{}
	adapter := &fakeAdapter{preflightErr: errors.New("adapter command not found")}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "preflight adapter") {
		t.Fatalf("Run error = %v, want preflight adapter error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateAdapterError)
	}
	if record.Iterations != 0 || len(comparator.configs) != 0 || len(adapter.instructions) != 0 {
		t.Fatalf("preflight failure performed real work: iterations=%d comparisons=%d fixes=%d", record.Iterations, len(comparator.configs), len(adapter.instructions))
	}
}

func TestRunRejectsAdapterTemperatureBeforeCandidateLifecycle(t *testing.T) {
	comparator := &fakeComparator{}
	candidate := &fakeCandidate{}
	adapter := &fakeAdapter{
		preflightResult: &AdapterResult{Temperature: float64Pointer(2.01)},
	}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
	})
	if err == nil || !strings.Contains(err.Error(), "temperature must be between 0 and 2") {
		t.Fatalf("Run() error = %v, want out-of-range temperature error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want adapter error", record.TerminalState)
	}
	if len(candidate.calls) != 0 {
		t.Fatalf("candidate calls = %#v, want no lifecycle side effects", candidate.calls)
	}
	if len(comparator.configs) != 0 {
		t.Fatalf("comparator calls = %d, want 0", len(comparator.configs))
	}
}

func TestRunIteratesThenConvergesAndPassesRenderedPrompt(t *testing.T) {
	first := agentreport.View{
		Converged: false,
		Counts:    agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	second := first
	third := agentreport.View{Converged: true, Counts: agentreport.Counts{Fixed: 1}}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
		{view: third, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{usages: []*benchrecord.TokenUsage{
		{Input: 1, Output: 2, Total: 3},
		{Input: 4, Output: 5, Total: 9},
	}}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if record.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", record.Iterations)
	}
	if len(adapter.instructions) != 2 {
		t.Fatalf("adapter calls = %d, want 2", len(adapter.instructions))
	}
	if !strings.Contains(adapter.instructions[0], `"problem-1"`) {
		t.Fatalf("rendered instruction does not contain the compact view: %q", adapter.instructions[0])
	}
	if !strings.Contains(adapter.instructions[0], "stbench-default@3") {
		t.Fatalf("rendered instruction does not contain the prompt identity: %q", adapter.instructions[0])
	}
	if got, want := *record.Tokens, (benchrecord.TokenUsage{Input: 5, Output: 7, Total: 12}); got != want {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
	if record.Final.StillFailing != 0 || record.Final.Converged != true {
		t.Fatalf("final summary = %#v, want converged with no remaining failures", record.Final)
	}
}

func TestRunRecordsPromptHashAndRenderedInstructions(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{responses: []string{"raw model response"}}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(record.Prompt.Hash) != 64 {
		t.Fatalf("prompt hash length = %d, want SHA-256 hex length", len(record.Prompt.Hash))
	}
	wantPromptHash := hashContent(promptTemplateText)
	if record.Prompt.Hash != wantPromptHash {
		t.Fatalf("prompt hash = %q, want embedded template hash %q", record.Prompt.Hash, wantPromptHash)
	}
	if len(record.PromptInstructions) != 1 {
		t.Fatalf("prompt instructions = %#v, want one rendered instruction", record.PromptInstructions)
	}
	if record.PromptInstructions[0] != adapter.instructions[0] {
		t.Fatalf("archived instruction = %q, want adapter instruction %q", record.PromptInstructions[0], adapter.instructions[0])
	}
	if strings.Contains(record.PromptInstructions[0], record.Prompt.Hash) {
		t.Fatalf("rendered instruction should not contain audit metadata: %q", record.PromptInstructions[0])
	}
	if len(record.RenderedPromptHashes) != 1 || len(record.RenderedPromptHashes[0]) != 64 {
		t.Fatalf("rendered prompt hashes = %#v, want one SHA-256 hash", record.RenderedPromptHashes)
	}
	if len(record.RenderedPromptBytes) != 1 || record.RenderedPromptBytes[0] != len(record.PromptInstructions[0]) {
		t.Fatalf("rendered prompt bytes = %#v, want rendered instruction byte length", record.RenderedPromptBytes)
	}
	if len(record.AgentResponses) != 1 || record.AgentResponses[0] != "raw model response" {
		t.Fatalf("agent responses = %#v, want archived raw model response", record.AgentResponses)
	}
}

func TestRunStopsBeforeAdapterWhenPromptExceedsMaxBytes(t *testing.T) {
	view := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Candidate:     "candidate",
		Counts:        agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	config := Config{
		Candidate:      "candidate",
		ReportsDir:     "reports",
		PromptMaxBytes: renderedPromptBytes(t, Config{Candidate: "candidate", ReportsDir: "reports"}, view) - 1,
		MaxIterations:  3,
		StallWindow:    2,
		BaselineExists: func() bool { return true },
	}
	comparator := &fakeComparator{results: []comparisonResult{{view: view, exitCode: agentreport.ExitCodeNotConverged}}}
	candidate := &fakeCandidate{}
	adapter := &fakeAdapter{}

	record, err := Run(config, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("Run error = %v, want prompt size error", err)
	}
	if record.TerminalState != benchrecord.TerminalStatePromptTooLarge {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStatePromptTooLarge)
	}
	if record.PromptSizeLimit == nil {
		t.Fatal("prompt_size_limit = nil, want observed size and cap")
	}
	if record.PromptSizeLimit.ObservedBytes <= record.PromptSizeLimit.MaxBytes ||
		record.PromptSizeLimit.MaxBytes != config.PromptMaxBytes {
		t.Fatalf("prompt_size_limit = %#v, want observed size above configured cap %d", record.PromptSizeLimit, config.PromptMaxBytes)
	}
	if len(record.RenderedPromptBytes) != 1 ||
		record.RenderedPromptBytes[0] != record.PromptSizeLimit.ObservedBytes {
		t.Fatalf("rendered prompt bytes = %#v, want observed prompt size", record.RenderedPromptBytes)
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter calls = %d, want 0", len(adapter.instructions))
	}
	if len(comparator.configs) != 1 {
		t.Fatalf("comparator calls = %d, want 1", len(comparator.configs))
	}
	if got, want := candidate.calls, preflightAndFirstIterationCalls; !sameStrings(got, want) {
		t.Fatalf("candidate calls = %#v, want lifecycle through the guarded iteration %#v", got, want)
	}
}

func TestRunAllowsPromptAtInclusiveMaxBytes(t *testing.T) {
	view := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Candidate:     "candidate",
		Counts:        agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	config := Config{Candidate: "candidate", ReportsDir: "reports"}
	config.PromptMaxBytes = renderedPromptBytes(t, config, view)
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}

	record, err := Run(config, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if len(adapter.instructions) != 1 {
		t.Fatalf("adapter calls = %d, want 1 at inclusive prompt limit", len(adapter.instructions))
	}
	if record.PromptSizeLimit != nil {
		t.Fatalf("prompt_size_limit = %#v, want nil when prompt is within inclusive limit", record.PromptSizeLimit)
	}
}

func TestRunRejectsNegativePromptMaxBytesBeforeLifecycle(t *testing.T) {
	candidate := &fakeCandidate{}
	comparator := &fakeComparator{}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		PromptMaxBytes: -1,
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "max_bytes") {
		t.Fatalf("Run error = %v, want prompt max_bytes configuration error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateToolError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateToolError)
	}
	if len(candidate.calls) != 0 || len(comparator.configs) != 0 || len(adapter.preflightMetadata) != 0 {
		t.Fatalf("negative prompt limit performed work: candidate=%#v comparator=%#v adapter=%#v", candidate.calls, comparator.configs, adapter.preflightMetadata)
	}
}

func renderedPromptBytes(t *testing.T, config Config, view agentreport.View) int {
	t.Helper()
	prompt := config.Prompt
	if prompt.ID == "" {
		prompt.ID = DefaultPromptID
	}
	if prompt.Version == "" {
		prompt.Version = DefaultPromptVersion
	}
	instruction, err := renderPromptTemplateAtPath(
		promptTemplate,
		prompt,
		view,
		comparisonPath(config.ReportsDir, config.Candidate),
	)
	if err != nil {
		t.Fatalf("render prompt fixture: %v", err)
	}
	return len(instruction)
}

func TestRenderPromptWrappersUseDefaultComparisonPath(t *testing.T) {
	prompt := benchrecord.PromptIdentity{ID: "stbench-default", Version: "3"}
	view := agentreport.View{Candidate: "candidate"}
	wantPath := "reports/candidate/comparison.json"

	renderedPrompt, err := renderPrompt(prompt, view)
	if err != nil {
		t.Fatalf("renderPrompt returned error: %v", err)
	}
	if !strings.Contains(renderedPrompt, wantPath) {
		t.Fatalf("renderPrompt = %q, want default comparison path %q", renderedPrompt, wantPath)
	}

	renderedTemplate, err := renderPromptTemplate(promptTemplate, prompt, view)
	if err != nil {
		t.Fatalf("renderPromptTemplate returned error: %v", err)
	}
	if !strings.Contains(renderedTemplate, wantPath) {
		t.Fatalf("renderPromptTemplate = %q, want default comparison path %q", renderedTemplate, wantPath)
	}
}

func TestRunRecordsV3PromptExplanationsAndConfiguredComparisonPath(t *testing.T) {
	view := agentreport.View{
		Candidate: "candidate",
		Counts:    agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID:            "group-1",
			Kind:          agentreport.ActionKindStillFailing,
			Operation:     "GET /widgets",
			CheckCategory: "response_schema_conformance",
			Count:         1,
			Refs:          []int{7},
			Sample: agentreport.Sample{
				Ref:          7,
				Operation:    "GET /widgets",
				RequestBody:  "{}",
				ResponseBody: "{}",
			},
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		Candidate:      "candidate",
		ReportsDir:     "configured-reports",
		Baseline:       "baseline",
		Prompt:         benchrecord.PromptIdentity{},
		MaxIterations:  2,
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.Prompt.Version != "3" {
		t.Fatalf("prompt version = %q, want 3", record.Prompt.Version)
	}
	if len(adapter.instructions) != 1 {
		t.Fatalf("adapter instructions = %d, want 1", len(adapter.instructions))
	}
	instruction := adapter.instructions[0]
	for _, fragment := range []string{
		"one Problem Group",
		"`count` is the number of failing cases in the group",
		"`sample` is one concrete case with truncated request and response bodies",
		"`refs` are interaction numbers in `configured-reports/candidate/comparison.json`",
	} {
		if !strings.Contains(instruction, fragment) {
			t.Fatalf("instruction missing %q:\n%s", fragment, instruction)
		}
	}
	for _, forbidden := range []string{"fix this group first", "map routes to files", "controller", "handler"} {
		if strings.Contains(strings.ToLower(instruction), forbidden) {
			t.Fatalf("instruction contains forbidden guidance %q:\n%s", forbidden, instruction)
		}
	}
}

func TestRunUsesCustomPromptFileFromWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	promptContent := "Custom\n{{ .ComparisonView }}\n"
	if err := os.WriteFile(filepath.Join(directory, "prompt.md"), []byte(promptContent), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}
	var notice bytes.Buffer
	config := testConfig()
	config.PromptFile = "prompt.md"

	record, err := Run(config, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
		Notice:     &notice,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := record.Prompt.Hash, "d7d38aa643f66b523788c3b0257575a6172ff98dc10dde81f883a0c828681160"; got != want {
		t.Fatalf("prompt hash = %q, want file content hash %q", got, want)
	}
	if len(adapter.instructions) != 1 || !strings.HasPrefix(adapter.instructions[0], "Custom\n") ||
		!strings.Contains(adapter.instructions[0], `"problem-1"`) {
		t.Fatalf("adapter instructions = %#v, want custom template rendered with comparison view", adapter.instructions)
	}
	if got, want := notice.String(), "using custom prompt template prompt.md\n"; got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

func TestRunRejectsInvalidCustomPromptBeforeLoop(t *testing.T) {
	directory := t.TempDir()
	tests := []struct {
		name        string
		path        string
		content     string
		wantMessage string
		writeFile   bool
	}{
		{
			name:        "missing file",
			path:        filepath.Join(directory, "missing.md"),
			wantMessage: "load custom prompt template",
		},
		{
			name:        "whitespace-only path",
			path:        "   ",
			wantMessage: "load custom prompt template",
		},
		{
			name:        "unparseable template",
			path:        filepath.Join(directory, "unparseable.md"),
			content:     "{{",
			wantMessage: "parse custom prompt template",
			writeFile:   true,
		},
		{
			name:        "missing comparison view",
			path:        filepath.Join(directory, "no-view.md"),
			content:     "{{/* .ComparisonView */}}Fix the candidate",
			wantMessage: "must reference .ComparisonView",
			writeFile:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.writeFile {
				if err := os.WriteFile(test.path, []byte(test.content), 0o644); err != nil {
					t.Fatalf("write prompt file: %v", err)
				}
			}
			comparator := &fakeComparator{}
			candidate := &fakeCandidate{}
			adapter := &fakeAdapter{}
			config := testConfig()
			config.PromptFile = test.path

			record, err := Run(config, Dependencies{
				Comparator: comparator,
				Candidate:  candidate,
				Adapter:    adapter,
			})
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("Run error = %v, want message containing %q", err, test.wantMessage)
			}
			if record.SchemaVersion != "" || len(comparator.configs) != 0 ||
				len(candidate.calls) != 0 || len(adapter.preflightMetadata) != 0 {
				t.Fatalf("invalid prompt started loop: record=%#v comparisons=%d candidate=%#v preflights=%d",
					record, len(comparator.configs), candidate.calls, len(adapter.preflightMetadata))
			}
		})
	}
}

func TestRunEmbeddedPromptDoesNotEmitCustomPromptNotice(t *testing.T) {
	var notice bytes.Buffer
	_, err := Run(Config{BaselineExists: func() bool { return true }}, Dependencies{
		Comparator: &fakeComparator{results: []comparisonResult{{
			view:     agentreport.View{Converged: true},
			exitCode: agentreport.ExitCodeConverged,
		}}},
		Candidate: &fakeCandidate{},
		Adapter:   &fakeAdapter{},
		Notice:    &notice,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if notice.Len() != 0 {
		t.Fatalf("notice = %q, want silence for embedded prompt", notice.String())
	}
}

func TestRunPassesRecordedMetadataToAdapter(t *testing.T) {
	view := agentreport.View{Counts: agentreport.Counts{StillFailing: 1}}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		AdapterMetadata: AdapterMetadata{
			Agent: "codex", Model: "gpt-5", Effort: "high", Hardware: "m4-pro",
			Temperature: float64Pointer(0.65),
		},
		BaselineExists: func() bool { return true },
		MaxIterations:  2,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(adapter.metadata) != 1 {
		t.Fatalf("adapter metadata calls = %d, want 1", len(adapter.metadata))
	}
	metadata := adapter.metadata[0]
	if metadata.Agent != "codex" || metadata.Model != "gpt-5" || metadata.Effort != "high" ||
		metadata.Hardware != "m4-pro" || metadata.Temperature == nil || *metadata.Temperature != 0.65 {
		t.Fatalf("adapter metadata = %#v, want campaign metadata with temperature", metadata)
	}
	if record.Temperature != 0.65 {
		t.Fatalf("record temperature = %v, want 0.65", record.Temperature)
	}
}

func TestRunRecordsAdapterReportedEffectiveTemperature(t *testing.T) {
	adapter := &fakeAdapter{
		preflightResult: &AdapterResult{Temperature: float64Pointer(0.9)},
	}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.Temperature != 0.9 {
		t.Fatalf("record temperature = %v, want adapter-reported effective temperature", record.Temperature)
	}
}

func TestRunRecordsAdapterReportedHistoryPolicy(t *testing.T) {
	adapter := &fakeAdapter{
		preflightResult: &AdapterResult{
			HistoryPolicy: &benchrecord.HistoryPolicy{ReadResults: "keep"},
		},
	}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.HistoryPolicy == nil || record.HistoryPolicy.ReadResults != "keep" {
		t.Fatalf("record history policy = %#v, want adapter-reported keep policy", record.HistoryPolicy)
	}
}

func TestRunRecordsAdapterReportedIdentity(t *testing.T) {
	for _, test := range []struct {
		name         string
		response     string
		wantIdentity *benchrecord.Adapter
	}{
		{
			name:     "reported",
			response: `{"status":"ok","adapter":{"name":"local","version":"1","source_sha256":"sha"}}`,
			wantIdentity: &benchrecord.Adapter{
				Name: "local", Version: "1", SourceSHA256: "sha",
			},
		},
		{
			name:         "omitted",
			response:     `{"status":"ok"}`,
			wantIdentity: nil,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			script := writeExecutable(t, directory, "adapter.sh", "#!/bin/sh\n"+
				"printf '%s' '"+test.response+"'\n")
			adapter := &CommandAdapter{Command: script, WorkingDir: directory}
			comparator := &fakeComparator{results: []comparisonResult{{
				view:     agentreport.View{Converged: true},
				exitCode: agentreport.ExitCodeConverged,
			}}}

			record, err := Run(Config{
				BaselineExists: func() bool { return true },
			}, Dependencies{
				Comparator: comparator,
				Candidate:  &fakeCandidate{},
				Adapter:    adapter,
			})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if record.Adapter == nil && test.wantIdentity == nil {
				return
			}
			if record.Adapter == nil || test.wantIdentity == nil || *record.Adapter != *test.wantIdentity {
				t.Fatalf("record adapter = %#v, want %#v", record.Adapter, test.wantIdentity)
			}
		})
	}
}

func TestRunOmitsAdapterWhenAdapterDoesNotReportIt(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.Adapter != nil {
		t.Fatalf("record adapter = %#v, want omitted", record.Adapter)
	}
}

func TestRunRecordsHistoryPolicyReportedByFix(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID:   "problem-1",
			Kind: agentreport.ActionKindStillFailing,
		}},
	}
	adapter := &fakeAdapter{
		historyPolicies: []*benchrecord.HistoryPolicy{{ReadResults: "keep"}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.HistoryPolicy == nil || record.HistoryPolicy.ReadResults != "keep" {
		t.Fatalf("record history policy = %#v, want fix-reported keep policy", record.HistoryPolicy)
	}
}

func TestRunRecordsAdapterReportedIdentityFromFix(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID:   "problem-1",
			Kind: agentreport.ActionKindStillFailing,
		}},
	}
	identity := &benchrecord.Adapter{Name: "local", Version: "1", SourceSHA256: "sha"}
	adapter := &fakeAdapter{adapterIdentities: []*benchrecord.Adapter{identity}}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.Adapter == nil || *record.Adapter != *identity {
		t.Fatalf("record adapter = %#v, want %#v", record.Adapter, identity)
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}

func TestRunStopsAtMaxIterations(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
	}}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  2,
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0))})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateMaxIterations {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateMaxIterations)
	}
	if len(adapter.instructions) != 1 {
		t.Fatalf("adapter calls = %d, want 1", len(adapter.instructions))
	}
	if len(record.RemainingActionable) != 1 || record.RemainingActionable[0].ID != "problem-1" {
		t.Fatalf("remaining actionable = %#v, want problem-1", record.RemainingActionable)
	}
}

func TestRunStopsOnStallAndMarksPersistentActionableItems(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateStalled)
	}
	if record.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", record.Iterations)
	}
	if len(record.RemainingActionable) != 1 {
		t.Fatalf("remaining actionable = %#v, want one item", record.RemainingActionable)
	}
	if item := record.RemainingActionable[0]; item.ID != "problem-1" || !item.Stuck {
		t.Fatalf("remaining actionable item = %#v, want persistent stuck item", item)
	}
}

func TestRunUsesCaseTotalForProgressWhenGroupCountIsConstant(t *testing.T) {
	views := []agentreport.View{
		caseTotalView(3),
		caseTotalView(2),
		caseTotalView(1),
		{SchemaVersion: agentreport.SchemaVersion, Converged: true},
	}
	results := make([]comparisonResult, len(views))
	for i, view := range views {
		exitCode := agentreport.ExitCodeNotConverged
		if view.Converged {
			exitCode = agentreport.ExitCodeConverged
		}
		results[i] = comparisonResult{view: view, exitCode: exitCode}
	}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  4,
		StallWindow:    2,
	}, Dependencies{
		Comparator: &fakeComparator{results: results},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want converged", record.TerminalState)
	}
	if record.Iterations != 4 {
		t.Fatalf("iterations = %d, want 4", record.Iterations)
	}
}

func TestRunStallsWhenCaseTotalDoesNotDecrease(t *testing.T) {
	view := caseTotalView(2)
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
		StallWindow:    2,
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: &fakeAdapter{}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled || record.Iterations != 3 {
		t.Fatalf("record = %#v, want stalled on iteration 3", record)
	}
}

func TestRunRecordsGroupedRemainingActionableFieldsAndStuckCounts(t *testing.T) {
	first := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Counts:        agentreport.Counts{StillFailing: 4},
		Actionable: []agentreport.Actionable{
			{ID: "decreasing", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets", CheckCategory: "schema", Count: 2},
			{ID: "unchanged", Kind: agentreport.ActionKindRegressed, Operation: "POST /widgets", CheckCategory: "status", Count: 1},
			{ID: "increasing", Kind: agentreport.ActionKindStillFailing, Operation: "PUT /widgets", CheckCategory: "body", Count: 1},
		},
	}
	second := first
	second.Actionable = []agentreport.Actionable{
		{ID: "decreasing", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets", CheckCategory: "schema", Count: 1},
		{ID: "unchanged", Kind: agentreport.ActionKindRegressed, Operation: "POST /widgets", CheckCategory: "status", Count: 1},
		{ID: "increasing", Kind: agentreport.ActionKindStillFailing, Operation: "PUT /widgets", CheckCategory: "body", Count: 2},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  3,
		StallWindow:    1,
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: &fakeAdapter{}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want stalled", record.TerminalState)
	}
	if len(record.RemainingActionable) != 3 {
		t.Fatalf("remaining actionable = %#v, want three groups", record.RemainingActionable)
	}
	decreasing := record.RemainingActionable[0]
	unchanged := record.RemainingActionable[1]
	increasing := record.RemainingActionable[2]
	if decreasing.ID != "decreasing" || decreasing.Count != 1 || decreasing.CheckCategory != "schema" || decreasing.Stuck {
		t.Fatalf("decreasing group = %#v, want count 1 and not stuck", decreasing)
	}
	if unchanged.ID != "unchanged" || unchanged.Kind != "regressed" || unchanged.Operation != "POST /widgets" ||
		unchanged.Count != 1 || unchanged.CheckCategory != "status" || !unchanged.Stuck {
		t.Fatalf("unchanged group = %#v, want grouped fields and stuck", unchanged)
	}
	if increasing.ID != "increasing" || increasing.Count != 2 || increasing.CheckCategory != "body" || !increasing.Stuck {
		t.Fatalf("increasing group = %#v, want count 2 and stuck", increasing)
	}
}

func TestRunRejectsUnsupportedAgentViewSchema(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{SchemaVersion: "1", Counts: agentreport.Counts{StillFailing: 1}},
		exitCode: agentreport.ExitCodeNotConverged,
	}}}
	adapter := &fakeAdapter{}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
	})
	if err == nil || !strings.Contains(err.Error(), `"1"`) || !strings.Contains(err.Error(), `"2"`) {
		t.Fatalf("Run error = %v, want observed and expected schema versions", err)
	}
	if record.TerminalState != benchrecord.TerminalStateToolError {
		t.Fatalf("terminal state = %q, want tool_error", record.TerminalState)
	}
	if record.AgentViewSchemaVersion != "1" {
		t.Fatalf("agent view schema version = %q, want observed v1", record.AgentViewSchemaVersion)
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter calls = %d, want no call for rejected view", len(adapter.instructions))
	}
}

func TestRunRejectsNegativeStallWindow(t *testing.T) {
	_, err := Run(Config{
		BaselineExists: func() bool { return true },
		StallWindow:    -1,
	}, Dependencies{
		Comparator: &fakeComparator{},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
	})
	if err == nil || !strings.Contains(err.Error(), "stall window") {
		t.Fatalf("Run error = %v, want negative stall-window error", err)
	}
}

func TestRunMarksNewlyIntroducedActionableItemsAsNotStuck(t *testing.T) {
	first := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	regressed := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1, Regressed: 1},
		Actionable: []agentreport.Actionable{
			{ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets"},
			{ID: "regression-1", Kind: agentreport.ActionKindRegressed, Operation: "POST /widgets"},
		},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: regressed, exitCode: agentreport.ExitCodeNotConverged},
		{view: regressed, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
		StallWindow:    1,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateStalled)
	}
	if len(record.RemainingActionable) != 2 {
		t.Fatalf("remaining actionable = %#v, want two items", record.RemainingActionable)
	}
	if !record.RemainingActionable[0].Stuck || record.RemainingActionable[1].Stuck {
		t.Fatalf("remaining actionable = %#v, want persistent then newly introduced", record.RemainingActionable)
	}
}

func TestRunWithDecreasingCountsConvergesWithoutStalling(t *testing.T) {
	first := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 2},
		Actionable: []agentreport.Actionable{
			{ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets"},
			{ID: "problem-2", Kind: agentreport.ActionKindStillFailing, Operation: "POST /widgets"},
		},
	}
	second := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	third := agentreport.View{Converged: true}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
		{view: third, exitCode: agentreport.ExitCodeConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  3,
		StallWindow:    2,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if record.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", record.Iterations)
	}
}

func TestRunMaxIterationsMarksPersistentAndNewItems(t *testing.T) {
	first := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	second := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1, Regressed: 1},
		Actionable: []agentreport.Actionable{
			{ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets"},
			{ID: "regression-1", Kind: agentreport.ActionKindRegressed, Operation: "POST /widgets"},
		},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  2,
		StallWindow:    5,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateMaxIterations {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateMaxIterations)
	}
	if !record.RemainingActionable[0].Stuck || record.RemainingActionable[1].Stuck {
		t.Fatalf("remaining actionable = %#v, want persistent then newly introduced", record.RemainingActionable)
	}
}

func TestRunMaxIterationsRemainsReachableForOscillatingCounts(t *testing.T) {
	views := []agentreport.View{
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}}},
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}, {ID: "problem-2"}}},
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}}},
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}, {ID: "problem-2"}}},
	}
	results := make([]comparisonResult, len(views))
	for i, view := range views {
		results[i] = comparisonResult{view: view, exitCode: agentreport.ExitCodeNotConverged}
	}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  4,
		StallWindow:    3,
	}, Dependencies{
		Comparator: &fakeComparator{results: results},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateMaxIterations {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateMaxIterations)
	}
}

func TestRunLargerStallWindowDelaysStall(t *testing.T) {
	view := agentreport.View{Actionable: []agentreport.Actionable{{ID: "problem-1"}}}
	results := make([]comparisonResult, 4)
	for i := range results {
		results[i] = comparisonResult{view: view, exitCode: agentreport.ExitCodeNotConverged}
	}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
		StallWindow:    3,
	}, Dependencies{
		Comparator: &fakeComparator{results: results},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateStalled)
	}
	if record.Iterations != 4 {
		t.Fatalf("iterations = %d, want 4", record.Iterations)
	}
}

func TestRunStopsOnComparatorToolError(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{exitCode: agentreport.ExitCodeToolError}}}
	adapter := &fakeAdapter{}

	record, err := Run(testConfig(), Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0))})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateToolError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateToolError)
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter calls = %d, want 0", len(adapter.instructions))
	}
}

func TestRunStopsOnAdapterError(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{exitCode: agentreport.ExitCodeNotConverged}}}
	adapter := &fakeAdapter{
		responses: []string{"partial raw model response"},
		errs:      []error{errors.New("agent failed")},
	}

	record, err := Run(testConfig(), Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0))})
	if err == nil || !strings.Contains(err.Error(), "agent failed") {
		t.Fatalf("Run error = %v, want adapter error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateAdapterError)
	}
	if len(record.AgentResponses) != 1 || record.AgentResponses[0] != "partial raw model response" {
		t.Fatalf("agent responses = %#v, want archived response from failed adapter", record.AgentResponses)
	}
}

func TestRunRecordsFailedCandidatePhase(t *testing.T) {
	candidate := &fakeCandidate{failPhase: "build", failErr: errors.New("compile failed")}
	record, err := Run(testConfig(), Dependencies{
		Comparator: &fakeComparator{}, Candidate: candidate, Adapter: &fakeAdapter{}, Now: fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "build") {
		t.Fatalf("Run error = %v, want build phase", err)
	}
	if record.TerminalState != benchrecord.TerminalStateLifecycleError || record.LifecyclePhase != "build" {
		t.Fatalf("lifecycle failure = state %q phase %q", record.TerminalState, record.LifecyclePhase)
	}
}

func TestRunTurnsLifecycleTimeoutIntoLifecycleError(t *testing.T) {
	candidate := &CommandCandidate{
		BuildCommand:   "sleep 1",
		CommandTimeout: 10 * time.Millisecond,
	}

	record, err := Run(testConfig(), Dependencies{
		Comparator: &fakeComparator{},
		Candidate:  candidate,
		Adapter:    &fakeAdapter{},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run error = %v, want lifecycle timeout", err)
	}
	if record.TerminalState != benchrecord.TerminalStateLifecycleError || record.LifecyclePhase != "build" {
		t.Fatalf("record = %#v, want build lifecycle error", record)
	}
}

func TestRunTurnsAdapterTimeoutIntoAdapterError(t *testing.T) {
	record, err := Run(testConfig(), Dependencies{
		Comparator: &fakeComparator{results: []comparisonResult{{
			view:     agentreport.View{Counts: agentreport.Counts{StillFailing: 1}},
			exitCode: agentreport.ExitCodeNotConverged,
		}}},
		Candidate: &fakeCandidate{},
		Adapter: &CommandAdapter{
			Command:        "sleep 1",
			CommandTimeout: 10 * time.Millisecond,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run error = %v, want adapter timeout", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateAdapterError)
	}
}

func TestRunMissingBaselineFailsBeforeCandidateLifecycle(t *testing.T) {
	candidate := &fakeCandidate{}
	comparator := &fakeComparator{}
	record, err := Run(Config{
		Baseline:       "baseline",
		BaselineExists: func() bool { return false },
		MaxIterations:  2,
	}, Dependencies{Comparator: comparator, Candidate: candidate, Adapter: &fakeAdapter{}, Now: fixedNow(time.Unix(0, 0))})
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("Run error = %v, want missing baseline error", err)
	}
	if record.Iterations != 0 || len(candidate.calls) != 0 || len(comparator.configs) != 0 {
		t.Fatalf("missing baseline performed work: iterations=%d candidate=%#v comparator=%#v", record.Iterations, candidate.calls, comparator.configs)
	}
	if record.LifecyclePhase != benchrecord.LifecyclePhaseBaselinePrecondition {
		t.Fatalf("lifecycle phase = %q, want %q", record.LifecyclePhase, benchrecord.LifecyclePhaseBaselinePrecondition)
	}
}

func TestRunTimeBreakdownSumsAndPartialTokens(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{usages: []*benchrecord.TokenUsage{{Input: 3, Output: 4, Total: 7}, nil}}
	now := advancingNow(10 * time.Millisecond)

	record, err := Run(Config{BaselineExists: func() bool { return true }, MaxIterations: 3}, Dependencies{
		Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: now,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := *record.Tokens, (benchrecord.TokenUsage{Input: 3, Output: 4, Total: 7}); got != want {
		t.Fatalf("tokens = %#v, want known partial sum %#v", got, want)
	}
	if got, want := record.UnknownTokenIterations, 1; got != want {
		t.Fatalf("unknown token iterations = %d, want %d", got, want)
	}
	if got := record.TimeMS.Total; got != record.TimeMS.CandidateReset+record.TimeMS.Compare+record.TimeMS.AgentFix {
		t.Fatalf("time total = %d, phase sum = %d", got, record.TimeMS.CandidateReset+record.TimeMS.Compare+record.TimeMS.AgentFix)
	}
}

func TestRunKeepsTokensNullWhenEveryFixOmitsTokenUsage(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{usages: []*benchrecord.TokenUsage{nil, nil}}

	record, err := Run(Config{BaselineExists: func() bool { return true }, MaxIterations: 3}, Dependencies{
		Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.Tokens != nil {
		t.Fatalf("tokens = %#v, want null when every fix omits usage", record.Tokens)
	}
	if got, want := record.UnknownTokenIterations, 2; got != want {
		t.Fatalf("unknown token iterations = %d, want %d", got, want)
	}
}

type comparisonResult struct {
	view     agentreport.View
	exitCode int
	err      error
}

type fakeComparator struct {
	results []comparisonResult
	configs []Config
}

func (f *fakeComparator) Compare(config Config) (agentreport.View, int, error) {
	f.configs = append(f.configs, config)
	result := f.results[0]
	f.results = f.results[1:]
	if result.view.SchemaVersion == "" {
		result.view.SchemaVersion = agentreport.SchemaVersion
	}
	return result.view, result.exitCode, result.err
}

type fakeCandidate struct {
	calls     []string
	failPhase string
	failErr   error
}

type sourceLifecycleCandidate struct {
	*fakeCandidate
	path       string
	buildCount int
}

func (candidate *sourceLifecycleCandidate) Build() error {
	candidate.calls = append(candidate.calls, "build")
	candidate.buildCount++
	contents := fmt.Sprintf("build-%d\n", candidate.buildCount)
	if err := os.WriteFile(candidate.path, []byte(contents), 0o644); err != nil {
		return err
	}
	return candidate.fail("build")
}

func (f *fakeCandidate) Stop() error {
	f.calls = append(f.calls, "stop")
	return f.fail("stop")
}

func (f *fakeCandidate) Reset() error {
	f.calls = append(f.calls, "reset")
	return f.fail("reset")
}

func (f *fakeCandidate) Build() error {
	f.calls = append(f.calls, "build")
	return f.fail("build")
}

func (f *fakeCandidate) Start() error {
	f.calls = append(f.calls, "start")
	return f.fail("start")
}

func (f *fakeCandidate) WaitHealthy() error {
	f.calls = append(f.calls, "wait_healthy")
	return f.fail("wait_healthy")
}

func (f *fakeCandidate) fail(phase string) error {
	if f.failPhase == phase {
		return f.failErr
	}
	return nil
}

type fakeAdapter struct {
	preflightMetadata []AdapterMetadata
	preflightResult   *AdapterResult
	preflightErr      error
	instructions      []string
	metadata          []AdapterMetadata
	usages            []*benchrecord.TokenUsage
	responses         []string
	historyPolicies   []*benchrecord.HistoryPolicy
	adapterIdentities []*benchrecord.Adapter
	errs              []error
}

type trackingAdapter struct {
	*fakeAdapter
	closeCalls   int
	processReuse bool
	closeErr     error
}

type artifactAdapter struct {
	*fakeAdapter
}

type invalidArtifactAdapter struct {
	*fakeAdapter
}

func (adapter *artifactAdapter) Preflight(metadata AdapterMetadata) error {
	if metadata.Audit != nil {
		contents := []byte(`{"schema_version":"1","run":{"id":"run-complete"},"capture":{"enabled":true,"status":"in_progress","complete":false},"iterations":[{"id":"iteration-1","number":1,"turn_ids":["iteration-1-turn-1"]}],"events":[{"sequence":1,"type":"model_turn","iteration_id":"iteration-1","status":"completed","input":{}}]}`)
		if err := os.WriteFile(metadata.Audit.Path, contents, 0o644); err != nil {
			return err
		}
	}
	return adapter.fakeAdapter.Preflight(metadata)
}

func (adapter *invalidArtifactAdapter) Preflight(metadata AdapterMetadata) error {
	if metadata.Audit != nil {
		if err := os.WriteFile(metadata.Audit.Path, []byte(`{"schema_version":"1"}`), 0o644); err != nil {
			return err
		}
	}
	return adapter.fakeAdapter.Preflight(metadata)
}

func (adapter *trackingAdapter) Close() error {
	adapter.closeCalls++
	return adapter.closeErr
}

func (adapter *trackingAdapter) ProcessReuseActive() bool {
	return adapter.processReuse
}

func (f *fakeAdapter) Preflight(metadata AdapterMetadata) error {
	f.preflightMetadata = append(f.preflightMetadata, metadata)
	return f.preflightErr
}

func (f *fakeAdapter) EffectiveTemperature() *float64 {
	if f.preflightResult == nil {
		return nil
	}
	return f.preflightResult.Temperature
}

func (f *fakeAdapter) EffectiveHistoryPolicy() *benchrecord.HistoryPolicy {
	if f.preflightResult == nil {
		return nil
	}
	return f.preflightResult.HistoryPolicy
}

func (f *fakeAdapter) EffectiveAdapter() *benchrecord.Adapter {
	if f.preflightResult == nil {
		return nil
	}
	return f.preflightResult.Adapter
}

func (f *fakeAdapter) Fix(
	instruction string,
	_ agentreport.View,
	metadata AdapterMetadata,
) (*AdapterResult, error) {
	f.instructions = append(f.instructions, instruction)
	f.metadata = append(f.metadata, metadata)
	var usage *benchrecord.TokenUsage
	if len(f.usages) != 0 {
		usage = f.usages[0]
		f.usages = f.usages[1:]
	}
	var response string
	if len(f.responses) != 0 {
		response = f.responses[0]
		f.responses = f.responses[1:]
	}
	var historyPolicy *benchrecord.HistoryPolicy
	if len(f.historyPolicies) != 0 {
		historyPolicy = f.historyPolicies[0]
		f.historyPolicies = f.historyPolicies[1:]
	}
	var adapterIdentity *benchrecord.Adapter
	if len(f.adapterIdentities) != 0 {
		adapterIdentity = f.adapterIdentities[0]
		f.adapterIdentities = f.adapterIdentities[1:]
	}
	var err error
	if len(f.errs) != 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return &AdapterResult{
		Tokens:        usage,
		Response:      response,
		HistoryPolicy: historyPolicy,
		Adapter:       adapterIdentity,
	}, err
}

func testConfig() Config {
	return Config{BaselineExists: func() bool { return true }, MaxIterations: 5, StallWindow: 3}
}

func caseTotalView(total int) agentreport.View {
	return agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Counts:        agentreport.Counts{StillFailing: total},
		Actionable: []agentreport.Actionable{{
			ID:    "group-1",
			Kind:  agentreport.ActionKindStillFailing,
			Count: total,
		}},
	}
}

var preflightAndFirstIterationCalls = []string{
	"stop", "reset", "build", "start", "wait_healthy", "stop",
	"stop", "reset", "build", "start", "wait_healthy",
}

func fixedNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func advancingNow(step time.Duration) func() time.Time {
	now := time.Unix(0, 0)
	return func() time.Time {
		current := now
		now = now.Add(step)
		return current
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
