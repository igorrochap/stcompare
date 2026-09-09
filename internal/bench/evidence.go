package bench

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
	"stcompare/internal/audit"
)

type runnerAuditEvidence struct {
	path          string
	source        *sourceTracker
	comparisons   []audit.ComparisonOutcome
	editSequences []audit.EditSequence
	pendingEdit   int
	hasPending    bool
}

func newRunnerAuditEvidence(config Config) *runnerAuditEvidence {
	if config.AuditPath == "" {
		return nil
	}
	evidence := &runnerAuditEvidence{path: config.AuditPath}
	if config.SourceDir != "" {
		evidence.source = newSourceTracker(config)
	}
	return evidence
}

func (evidence *runnerAuditEvidence) captureStartingSource() {
	if evidence == nil || evidence.source == nil {
		return
	}
	evidence.source.captureStarting()
}

func (evidence *runnerAuditEvidence) runLifecycle(phase string, action func() error) error {
	if evidence == nil || evidence.source == nil {
		return action()
	}
	return evidence.source.runLifecycle(phase, action)
}

func (evidence *runnerAuditEvidence) auditLifecycle(
	phase benchrecord.LifecyclePhase,
	action func() error,
) error {
	return evidence.runLifecycle(string(phase), action)
}

func (evidence *runnerAuditEvidence) recordComparison(
	runID string,
	iteration int,
	view agentreport.View,
	exitCode int,
	startedAt, endedAt time.Time,
	compareErr error,
) {
	if evidence == nil {
		return
	}
	viewJSON, err := json.Marshal(view)
	if err != nil {
		viewJSON = nil
	}
	status := "completed"
	if compareErr != nil {
		status = "failed"
	}
	outcome := audit.ComparisonOutcome{
		Sequence:    len(evidence.comparisons) + 1,
		ID:          fmt.Sprintf("comparison-%d", len(evidence.comparisons)+1),
		RunID:       runID,
		IterationID: fmt.Sprintf("iteration-%d", iteration),
		Iteration:   iteration,
		Status:      status,
		StartedAt:   startedAt.UTC().Format(time.RFC3339Nano),
		EndedAt:     endedAt.UTC().Format(time.RFC3339Nano),
		DurationMS:  endedAt.Sub(startedAt).Milliseconds(),
		ExitCode:    exitCode,
		View:        viewJSON,
	}
	if compareErr != nil {
		outcome.Error = compareErr.Error()
	}
	evidence.comparisons = append(evidence.comparisons, outcome)
	evidence.linkPendingEdit(outcome)
}

func (evidence *runnerAuditEvidence) linkPendingEdit(outcome audit.ComparisonOutcome) {
	if !evidence.hasPending {
		return
	}
	sequence := &evidence.editSequences[evidence.pendingEdit]
	sequence.SubsequentComparisonID = outcome.ID
	if outcome.Status == "completed" {
		sequence.EvaluationStatus = "evaluated"
	}
	evidence.hasPending = false
}

func (evidence *runnerAuditEvidence) beginEditSequence(
	runID string,
	iteration int,
	problemInput string,
	comparisonID string,
) {
	if evidence == nil {
		return
	}
	sequence := audit.EditSequence{
		Sequence:           len(evidence.editSequences) + 1,
		ID:                 fmt.Sprintf("edit-sequence-%d", len(evidence.editSequences)+1),
		RunID:              runID,
		IterationID:        fmt.Sprintf("iteration-%d", iteration),
		Iteration:          iteration,
		ProblemInput:       problemInput,
		ComparisonBeforeID: comparisonID,
		EvaluationStatus:   "not_evaluated",
	}
	evidence.editSequences = append(evidence.editSequences, sequence)
	evidence.pendingEdit = len(evidence.editSequences) - 1
	evidence.hasPending = true
}

func (evidence *runnerAuditEvidence) finalSource() audit.FinalSource {
	if evidence == nil || evidence.source == nil {
		return audit.FinalSource{}
	}
	return evidence.source.finalSource()
}

func (evidence *runnerAuditEvidence) lastComparisonID() string {
	if evidence == nil || len(evidence.comparisons) == 0 {
		return ""
	}
	return evidence.comparisons[len(evidence.comparisons)-1].ID
}

func (evidence *runnerAuditEvidence) append() error {
	if evidence == nil {
		return nil
	}
	if exists, err := auditArtifactExists(evidence.path); err != nil {
		return fmt.Errorf("inspect audit artifact for runner evidence: %w", err)
	} else if !exists {
		return nil
	}
	return audit.AppendEvidence(evidence.path, audit.Evidence{
		FinalSource:        evidence.finalSource(),
		ComparisonOutcomes: evidence.comparisons,
		EditSequences:      evidence.editSequences,
		LifecycleChanges:   evidence.lifecycleChanges(),
	})
}

func (evidence *runnerAuditEvidence) lifecycleChanges() []audit.SourceChange {
	if evidence == nil || evidence.source == nil {
		return nil
	}
	return evidence.source.lifecycleChanges()
}

type sourceTracker struct {
	root                   string
	excludes               []string
	starting               audit.SourceSnapshot
	hasStarting            bool
	lifecycle              []audit.SourceChange
	startingLifecycleIndex int
}

func newSourceTracker(config Config) *sourceTracker {
	return &sourceTracker{
		root:      config.SourceDir,
		excludes:  sourceExcludes(config),
		lifecycle: []audit.SourceChange{},
	}
}

func sourceExcludes(config Config) []string {
	excludes := []string{config.AuditPath, config.AuditReportPath}
	root, rootErr := filepath.Abs(config.SourceDir)
	auditDir, dirErr := filepath.Abs(filepath.Dir(config.AuditPath))
	if rootErr == nil && dirErr == nil && root != auditDir && isSourcePathWithin(root, auditDir) {
		excludes = append(excludes, auditDir)
	}
	return excludes
}

func isSourcePathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (tracker *sourceTracker) captureStarting() {
	tracker.starting = audit.CaptureSource(tracker.root, tracker.excludes)
	tracker.hasStarting = true
	tracker.startingLifecycleIndex = len(tracker.lifecycle)
}

func (tracker *sourceTracker) runLifecycle(phase string, action func() error) error {
	before := tracker.captureCurrent()
	err := action()
	after := tracker.captureCurrent()
	tracker.recordLifecycleChanges(phase, before, after)
	return err
}

func (tracker *sourceTracker) captureCurrent() audit.SourceSnapshot {
	return audit.CaptureSource(tracker.root, tracker.excludes)
}

func (tracker *sourceTracker) recordLifecycleChanges(
	phase string,
	before, after audit.SourceSnapshot,
) {
	transition := audit.BuildFinalSource(before, after, nil, nil)
	for _, change := range transition.Diffs {
		change.Sequence = len(tracker.lifecycle) + 1
		change.ID = fmt.Sprintf("lifecycle-change-%d", len(tracker.lifecycle)+1)
		change.Phase = phase
		change.Origin = audit.ChangeOriginLifecycle
		tracker.lifecycle = append(tracker.lifecycle, change)
	}
}

func (tracker *sourceTracker) lifecycleChanges() []audit.SourceChange {
	return append([]audit.SourceChange(nil), tracker.lifecycle...)
}

func (tracker *sourceTracker) finalSource() audit.FinalSource {
	final := audit.CaptureSource(tracker.root, tracker.excludes)
	if !tracker.hasStarting {
		return audit.FinalSource{
			Status: audit.SourceStatusUnavailable,
			Final:  final,
		}
	}
	return audit.BuildFinalSource(
		tracker.starting,
		final,
		tracker.lifecycle[tracker.startingLifecycleIndex:],
		nil,
	)
}
