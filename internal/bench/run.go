// Package bench drives one neutral benchmark fix loop.
package bench

import (
	"encoding/json"
	"fmt"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
)

const (
	// DefaultPromptID identifies the canonical stbench task prompt.
	DefaultPromptID = "stbench-default"
	// DefaultPromptVersion identifies the current canonical task prompt.
	DefaultPromptVersion = "1"
	// DefaultMaxIterations bounds runs that do not provide an explicit cap.
	DefaultMaxIterations = 100
	// DefaultStallWindow is the number of consecutive non-improving transitions
	// tolerated before a run stalls.
	DefaultStallWindow = 2
)

// Config describes one benchmark run.
type Config struct {
	Agent     string
	Model     string
	Hardware  string
	Candidate string
	Baseline  string

	// Prompt identifies the versioned task prompt. A zero value uses the
	// canonical prompt identity.
	Prompt benchrecord.PromptIdentity

	// BaselineExists is checked before any candidate lifecycle operation. A nil
	// check means the caller has already established the precondition.
	BaselineExists func() bool

	// MaxIterations is the hard number of comparisons allowed in a run. A zero
	// value uses DefaultMaxIterations.
	MaxIterations int

	// StallWindow is the number of consecutive transitions without a strict
	// actionable-count decrease allowed before a run stalls. A zero value uses
	// DefaultStallWindow.
	StallWindow int
}

// Dependencies contains the replaceable collaborators used by Run.
type Dependencies struct {
	Comparator Comparator
	Candidate  Candidate
	Adapter    Adapter
	Now        func() time.Time
}

// Comparator runs one comparison and returns its compact view and exit code.
type Comparator interface {
	Compare(config Config) (agentreport.View, int, error)
}

// Candidate manages the clean candidate lifecycle for one comparison.
type Candidate interface {
	Stop() error
	Reset() error
	Build() error
	Start() error
	WaitHealthy() error
}

// Adapter applies one rendered task instruction to the candidate.
type Adapter interface {
	Fix(instruction string, view agentreport.View) (*benchrecord.TokenUsage, error)
}

// Run drives a candidate until convergence or a terminal condition.
func Run(config Config, dependencies Dependencies) (benchrecord.Record, error) {
	if err := validate(config, dependencies); err != nil {
		return benchrecord.Record{}, err
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}

	if config.MaxIterations == 0 {
		config.MaxIterations = DefaultMaxIterations
	}
	if config.StallWindow == 0 {
		config.StallWindow = DefaultStallWindow
	}
	if config.Prompt.ID == "" {
		config.Prompt.ID = DefaultPromptID
	}
	if config.Prompt.Version == "" {
		config.Prompt.Version = DefaultPromptVersion
	}

	startedAt := dependencies.Now()
	record := benchrecord.Record{
		SchemaVersion: benchrecord.SchemaVersion,
		Agent:         config.Agent,
		Model:         config.Model,
		Hardware:      config.Hardware,
		Prompt:        config.Prompt,
		Candidate:     config.Candidate,
		Baseline:      config.Baseline,
		StartedAt:     startedAt.Format(time.RFC3339Nano),
		Tokens:        &benchrecord.TokenUsage{},
		Final:         benchrecord.FinalSummary{},
	}

	if config.BaselineExists != nil && !config.BaselineExists() {
		record.LifecyclePhase = benchrecord.LifecyclePhaseBaselinePrecondition
		return finish(record, dependencies.Now(), benchrecord.TerminalStateLifecycleError),
			fmt.Errorf("baseline precondition: campaign %q is missing", config.Baseline)
	}

	return runIterations(config, dependencies, record, startedAt)
}

func runIterations(
	config Config,
	dependencies Dependencies,
	record benchrecord.Record,
	startedAt time.Time,
) (benchrecord.Record, error) {
	runner := iterationRunner{
		config:       config,
		dependencies: dependencies,
		record:       &record,
		timer:        &phaseTimer{now: dependencies.Now, cursor: startedAt},
		tokensKnown:  true,
	}
	for iteration := 1; iteration <= config.MaxIterations; iteration++ {
		runner.record.Iterations = iteration
		done, err := runner.runIteration(iteration == config.MaxIterations)
		if done {
			return *runner.record, err
		}
	}

	panic("unreachable: benchmark loop exhausted")
}

type iterationRunner struct {
	config       Config
	dependencies Dependencies
	record       *benchrecord.Record
	timer        *phaseTimer
	lastView     agentreport.View
	progress     progressTracker
	tokensKnown  bool
}

func (runner *iterationRunner) runIteration(lastIteration bool) (bool, error) {
	if err := runner.timer.run(&runner.record.TimeMS.CandidateReset, func() error {
		return runCandidateLifecycle(runner.dependencies.Candidate, &runner.record.LifecyclePhase)
	}); err != nil {
		runner.record.Final = finalSummary(runner.lastView)
		return true, runner.bail(benchrecord.TerminalStateLifecycleError, err)
	}

	var view agentreport.View
	var exitCode int
	if err := runner.timer.run(&runner.record.TimeMS.Compare, func() error {
		var err error
		view, exitCode, err = runner.dependencies.Comparator.Compare(runner.config)
		return err
	}); err != nil {
		runner.record.Final = finalSummary(view)
		return true, runner.bail(
			benchrecord.TerminalStateToolError,
			fmt.Errorf("compare: %w", err),
		)
	}
	runner.lastView = view
	runner.record.Final = finalSummary(view)
	stalled := runner.progress.observe(view, runner.config.StallWindow)

	switch exitCode {
	case agentreport.ExitCodeConverged:
		runner.record.RemainingActionable = []benchrecord.ActionableItem{}
		runner.complete(benchrecord.TerminalStateConverged)
		return true, nil
	case agentreport.ExitCodeToolError:
		runner.complete(benchrecord.TerminalStateToolError)
		return true, nil
	case agentreport.ExitCodeNotConverged:
		if lastIteration {
			runner.record.RemainingActionable = actionableItems(view, runner.progress.stuckIDs())
			runner.complete(benchrecord.TerminalStateMaxIterations)
			return true, nil
		}
		if stalled {
			runner.record.RemainingActionable = actionableItems(view, runner.progress.stuckIDs())
			runner.complete(benchrecord.TerminalStateStalled)
			return true, nil
		}
	default:
		return true, runner.bail(
			benchrecord.TerminalStateToolError,
			fmt.Errorf("compare: unexpected exit code %d", exitCode),
		)
	}

	if err := runner.timer.run(&runner.record.TimeMS.AgentFix, func() error {
		return runAgentFix(
			runner.dependencies.Adapter,
			runner.config.Prompt,
			view,
			runner.record.Tokens,
			&runner.tokensKnown,
		)
	}); err != nil {
		return true, runner.bail(benchrecord.TerminalStateAdapterError, err)
	}
	return false, nil
}

func (runner *iterationRunner) bail(state benchrecord.TerminalState, err error) error {
	runner.record.Tokens = tokenRecord(runner.tokensKnown, runner.record.Tokens)
	*runner.record = finish(*runner.record, runner.timer.current(), state)
	return err
}

func (runner *iterationRunner) complete(state benchrecord.TerminalState) {
	runner.record.Tokens = tokenRecord(runner.tokensKnown, runner.record.Tokens)
	*runner.record = finish(*runner.record, runner.timer.current(), state)
}

func validate(config Config, dependencies Dependencies) error {
	if dependencies.Comparator == nil {
		return fmt.Errorf("comparator dependency is required")
	}
	if dependencies.Candidate == nil {
		return fmt.Errorf("candidate dependency is required")
	}
	if dependencies.Adapter == nil {
		return fmt.Errorf("adapter dependency is required")
	}
	if config.MaxIterations < 0 {
		return fmt.Errorf("max iterations must not be negative")
	}
	if config.StallWindow < 0 {
		return fmt.Errorf("stall window must not be negative")
	}
	return nil
}

func runCandidateLifecycle(candidate Candidate, failedPhase *benchrecord.LifecyclePhase) error {
	phases := []struct {
		name benchrecord.LifecyclePhase
		call func() error
	}{
		{name: benchrecord.LifecyclePhaseStop, call: candidate.Stop},
		{name: benchrecord.LifecyclePhaseReset, call: candidate.Reset},
		{name: benchrecord.LifecyclePhaseBuild, call: candidate.Build},
		{name: benchrecord.LifecyclePhaseStart, call: candidate.Start},
		{name: benchrecord.LifecyclePhaseWaitHealthy, call: candidate.WaitHealthy},
	}
	for _, phase := range phases {
		if err := phase.call(); err != nil {
			*failedPhase = phase.name
			return fmt.Errorf("candidate %s: %w", phase.name, err)
		}
	}
	return nil
}

func renderPrompt(prompt benchrecord.PromptIdentity, view agentreport.View) (string, error) {
	compactView, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"Task prompt %s@%s:\nUse the comparison result below to fix the candidate source. Apply the necessary fixes and preserve existing behavior outside the reported problems.\n\nComparison view:\n%s",
		prompt.ID,
		prompt.Version,
		compactView,
	), nil
}

func runAgentFix(
	adapter Adapter,
	prompt benchrecord.PromptIdentity,
	view agentreport.View,
	tokens *benchrecord.TokenUsage,
	tokensKnown *bool,
) error {
	instruction, err := renderPrompt(prompt, view)
	if err != nil {
		return fmt.Errorf("render task prompt: %w", err)
	}
	usage, err := adapter.Fix(instruction, view)
	if usage == nil {
		*tokensKnown = false
	} else if *tokensKnown {
		tokens.Input += usage.Input
		tokens.Output += usage.Output
		tokens.Total += usage.Total
	}
	if err != nil {
		return fmt.Errorf("adapter fix: %w", err)
	}
	return nil
}

func finalSummary(view agentreport.View) benchrecord.FinalSummary {
	return benchrecord.FinalSummary{
		Converged:    view.Converged,
		StillFailing: view.Counts.StillFailing,
		Regressed:    view.Counts.Regressed,
		Unverified: benchrecord.UnverifiedSummary{
			Inconclusive: view.Unverified.Inconclusive,
			Uncorrelated: view.Unverified.Uncorrelated,
			Ambiguous:    view.Unverified.Ambiguous,
			Unevaluable:  view.Unverified.Unevaluable,
		},
	}
}

func actionableItems(view agentreport.View, stuckIDs map[string]struct{}) []benchrecord.ActionableItem {
	items := make([]benchrecord.ActionableItem, len(view.Actionable))
	for i, item := range view.Actionable {
		items[i] = benchrecord.ActionableItem{
			ID:        item.ID,
			Kind:      string(item.Kind),
			Operation: item.Operation,
			Stuck:     containsID(stuckIDs, item.ID),
		}
	}
	return items
}

type progressTracker struct {
	previousCount int
	hasPrevious   bool
	nonImproving  int
	persistentIDs map[string]struct{}
}

func (tracker *progressTracker) observe(view agentreport.View, stallWindow int) bool {
	currentIDs := actionableIDs(view)
	currentCount := len(view.Actionable)
	if !tracker.hasPrevious {
		tracker.hasPrevious = true
		tracker.previousCount = currentCount
		tracker.persistentIDs = currentIDs
		return false
	}

	if currentCount < tracker.previousCount {
		tracker.nonImproving = 0
		tracker.persistentIDs = currentIDs
	} else {
		tracker.nonImproving++
		tracker.persistentIDs = intersectIDs(tracker.persistentIDs, currentIDs)
	}
	tracker.previousCount = currentCount

	return tracker.nonImproving >= stallWindow
}

func (tracker progressTracker) stuckIDs() map[string]struct{} {
	if tracker.nonImproving == 0 {
		return nil
	}
	return tracker.persistentIDs
}

func actionableIDs(view agentreport.View) map[string]struct{} {
	ids := make(map[string]struct{}, len(view.Actionable))
	for _, item := range view.Actionable {
		ids[item.ID] = struct{}{}
	}
	return ids
}

func intersectIDs(left, right map[string]struct{}) map[string]struct{} {
	intersection := make(map[string]struct{})
	for id := range left {
		if _, ok := right[id]; ok {
			intersection[id] = struct{}{}
		}
	}
	return intersection
}

func containsID(ids map[string]struct{}, id string) bool {
	_, ok := ids[id]
	return ok
}

func tokenRecord(known bool, usage *benchrecord.TokenUsage) *benchrecord.TokenUsage {
	if !known {
		return nil
	}
	return usage
}

func elapsedMS(ended, started time.Time) int64 {
	return ended.Sub(started).Milliseconds()
}

type phaseTimer struct {
	now    func() time.Time
	cursor time.Time
}

func (timer *phaseTimer) run(target *int64, phase func() error) error {
	started := timer.cursor
	err := phase()
	ended := timer.now()
	*target += elapsedMS(ended, started)
	timer.cursor = ended
	return err
}

func (timer phaseTimer) current() time.Time {
	return timer.cursor
}

func finish(record benchrecord.Record, endedAt time.Time, state benchrecord.TerminalState) benchrecord.Record {
	record.TerminalState = state
	record.EndedAt = endedAt.Format(time.RFC3339Nano)
	record.TimeMS.Total = endedAt.Sub(parseTime(record.StartedAt)).Milliseconds()
	return record
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
