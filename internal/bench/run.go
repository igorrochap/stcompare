// Package bench drives one neutral benchmark fix loop.
package bench

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"text/template"
	"text/template/parse"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
)

//go:embed prompt.md
var promptTemplateText string

var promptTemplate = template.Must(template.New("stbench-prompt").Parse(promptTemplateText))

var promptTemplateHash = hashContent(promptTemplateText)

const (
	// DefaultPromptID identifies the canonical stbench task prompt.
	DefaultPromptID = "stbench-default"
	// DefaultPromptVersion identifies the current canonical task prompt.
	DefaultPromptVersion = "2"
	// DefaultMaxIterations bounds runs that do not provide an explicit cap.
	DefaultMaxIterations = 100
	// DefaultStallWindow is the number of consecutive non-improving transitions
	// tolerated before a run stalls.
	DefaultStallWindow = 2
)

// AdapterMetadata identifies the execution configuration supplied to an adapter.
type AdapterMetadata struct {
	Agent       string   `json:"agent"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
	Temperature *float64 `json:"temperature,omitempty"`
	Hardware    string   `json:"hardware"`
}

// Config describes one benchmark run.
type Config struct {
	AdapterMetadata
	Candidate string
	Baseline  string

	// Prompt identifies the versioned task prompt. A zero value uses the
	// canonical prompt identity.
	Prompt benchrecord.PromptIdentity
	// PromptFile selects an external task prompt template. A zero value uses the
	// canonical embedded template. Relative paths resolve against the process's
	// current working directory.
	PromptFile string

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

	// ReuseProcess requests a negotiated long-lived adapter process. Adapters
	// that do not support the protocol fall back to cold invocations.
	ReuseProcess bool

	// HeartbeatInterval is the cadence of ProgressWaiting events emitted while
	// the agent fix is in flight. A zero value disables the heartbeat, keeping
	// the loop silent between phase boundaries.
	HeartbeatInterval time.Duration
}

// Dependencies contains the replaceable collaborators used by Run.
type Dependencies struct {
	Comparator Comparator
	Candidate  Candidate
	Adapter    Adapter
	Now        func() time.Time
	// Notice receives run-start notices. A nil writer keeps Run silent.
	Notice io.Writer

	// Reporter, when set, is narrated at every loop phase boundary. A nil
	// Reporter keeps Run silent, preserving its pure-library behavior.
	Reporter Reporter

	// ChangeInspector, when set, is polled on agent-fix heartbeat ticks to
	// narrate the files edited so far. Optional and independent of Reporter.
	ChangeInspector ChangeInspector
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

// Adapter applies one rendered task instruction and reports the adapter result.
type Adapter interface {
	Preflight(metadata AdapterMetadata) error
	Fix(instruction string, view agentreport.View, metadata AdapterMetadata) (*AdapterResult, error)
}

// EffectiveTemperatureReporter exposes an adapter's resolved run temperature
// when it can apply an adapter-level override during preflight.
type EffectiveTemperatureReporter interface {
	EffectiveTemperature() *float64
}

// AdapterCloser releases adapter resources after a benchmark run.
type AdapterCloser interface {
	Close() error
}

// Progress phase identifiers narrated to a Reporter. Lifecycle phases reuse the
// benchrecord.LifecyclePhase values so record and narration stay aligned.
const (
	ProgressPhaseIteration = "iteration"
	ProgressPhasePreflight = "preflight"
	ProgressPhaseCompare   = "compare"
	ProgressPhaseAgentFix  = "agent_fix"
	ProgressPhaseTerminal  = "terminal"
)

// Progress state identifiers for a ProgressEvent.
const (
	ProgressStart = "start"
	ProgressDone  = "done"
	ProgressError = "error"
	// ProgressWaiting is emitted periodically while a long phase (the agent
	// fix) is still in flight, so the run is visibly alive rather than silent.
	ProgressWaiting = "waiting"
)

// ProgressEvent describes one narrated transition in the fix loop. Compare
// fields are populated only on a ProgressPhaseCompare done event; Terminal is
// populated only on a ProgressPhaseTerminal event. Elapsed and ChangedFiles are
// populated on ProgressWaiting heartbeat events.
type ProgressEvent struct {
	Iteration    int
	Phase        string
	State        string
	Actionable   int
	Converged    bool
	StillFailing int
	Terminal     benchrecord.TerminalState
	Elapsed      time.Duration
	ChangedFiles []string
	Err          error
}

// Reporter observes loop progress. A heartbeat ticker may call Report from a
// separate goroutine while the agent fix is in flight; between phases only the
// loop goroutine calls it, and the two never overlap. Implementations must not
// block.
type Reporter interface {
	Report(ProgressEvent)
}

// ChangeInspector reports the candidate source paths an agent has modified so
// far. It is polled on heartbeat ticks during the agent fix so edits surface as
// they land. A nil inspector omits file narration.
type ChangeInspector interface {
	Changed() ([]string, error)
}

func report(reporter Reporter, event ProgressEvent) {
	if reporter == nil {
		return
	}
	reporter.Report(event)
}

// ProcessReuseReporter reports whether the adapter negotiated process reuse.
type ProcessReuseReporter interface {
	ProcessReuseActive() bool
}

// Run drives a candidate until convergence or a terminal condition.
func Run(config Config, dependencies Dependencies) (record benchrecord.Record, runErr error) {
	if err := validate(config, dependencies); err != nil {
		return benchrecord.Record{}, err
	}
	selectedPrompt, selectedPromptHash, err := loadPromptTemplate(config.PromptFile)
	if err != nil {
		return benchrecord.Record{}, err
	}
	if config.PromptFile != "" && dependencies.Notice != nil {
		fmt.Fprintf(dependencies.Notice, "using custom prompt template %s\n", config.PromptFile)
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
	config.Prompt.Hash = selectedPromptHash

	startedAt := dependencies.Now()
	record = benchrecord.Record{
		SchemaVersion:        benchrecord.SchemaVersion,
		Agent:                config.Agent,
		Model:                config.Model,
		Effort:               config.Effort,
		Temperature:          effectiveTemperature(config.Temperature),
		Hardware:             config.Hardware,
		Prompt:               config.Prompt,
		Candidate:            config.Candidate,
		Baseline:             config.Baseline,
		StartedAt:            startedAt.Format(time.RFC3339Nano),
		PromptInstructions:   []string{},
		RenderedPromptHashes: []string{},
		AgentResponses:       []string{},
		ProcessReuse:         config.ReuseProcess,
		Final:                benchrecord.FinalSummary{},
	}
	defer func() {
		if reporter, ok := dependencies.Adapter.(ProcessReuseReporter); ok {
			record.ProcessReuse = reporter.ProcessReuseActive()
		}
		if closer, ok := dependencies.Adapter.(AdapterCloser); ok {
			if err := closer.Close(); err != nil {
				closeErr := fmt.Errorf("close adapter: %w", err)
				if runErr == nil {
					if record.TerminalState != benchrecord.TerminalStateConverged {
						record = finish(record, dependencies.Now(), benchrecord.TerminalStateAdapterError)
					}
					runErr = closeErr
				} else {
					runErr = errors.Join(runErr, closeErr)
				}
			}
		}
	}()

	if config.BaselineExists != nil && !config.BaselineExists() {
		record.LifecyclePhase = benchrecord.LifecyclePhaseBaselinePrecondition
		return finish(record, dependencies.Now(), benchrecord.TerminalStateLifecycleError),
			fmt.Errorf("baseline precondition: campaign %q is missing", config.Baseline)
	}

	report(dependencies.Reporter, ProgressEvent{Phase: ProgressPhasePreflight, State: ProgressStart})
	if state, err := runPreflight(
		config,
		dependencies,
		&record.LifecyclePhase,
		&record.Temperature,
	); err != nil {
		report(dependencies.Reporter, ProgressEvent{Phase: ProgressPhasePreflight, State: ProgressError, Err: err})
		return finish(record, dependencies.Now(), state), err
	}
	report(dependencies.Reporter, ProgressEvent{Phase: ProgressPhasePreflight, State: ProgressDone})
	if reporter, ok := dependencies.Adapter.(ProcessReuseReporter); ok {
		record.ProcessReuse = reporter.ProcessReuseActive()
	}

	return runIterations(config, dependencies, selectedPrompt, record, startedAt)
}

func runIterations(
	config Config,
	dependencies Dependencies,
	promptTemplate *template.Template,
	record benchrecord.Record,
	startedAt time.Time,
) (benchrecord.Record, error) {
	record.Tokens = &benchrecord.TokenUsage{}
	runner := iterationRunner{
		config:         config,
		dependencies:   dependencies,
		promptTemplate: promptTemplate,
		record:         &record,
		timer:          &phaseTimer{now: dependencies.Now, cursor: startedAt},
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
	config                 Config
	dependencies           Dependencies
	promptTemplate         *template.Template
	record                 *benchrecord.Record
	timer                  *phaseTimer
	lastView               agentreport.View
	progress               progressTracker
	unknownTokenIterations int
	hasKnownTokens         bool
}

func (runner *iterationRunner) report(event ProgressEvent) {
	event.Iteration = runner.record.Iterations
	report(runner.dependencies.Reporter, event)
}

// withAgentFixHeartbeat runs fix while emitting ProgressWaiting events on the
// configured cadence, so a long agent invocation is visibly alive. The ticker
// goroutine is the only caller of report during fix; it is stopped and drained
// before this returns, so it never overlaps the surrounding start/done events.
func (runner *iterationRunner) withAgentFixHeartbeat(fix func() error) error {
	interval := runner.config.HeartbeatInterval
	if interval <= 0 || runner.dependencies.Reporter == nil {
		return fix()
	}

	started := runner.dependencies.Now()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var changed []string
				if inspector := runner.dependencies.ChangeInspector; inspector != nil {
					changed, _ = inspector.Changed()
				}
				runner.report(ProgressEvent{
					Phase:        ProgressPhaseAgentFix,
					State:        ProgressWaiting,
					Elapsed:      runner.dependencies.Now().Sub(started),
					ChangedFiles: changed,
				})
			}
		}
	}()

	err := fix()
	close(stop)
	<-done
	return err
}

func (runner *iterationRunner) runIteration(lastIteration bool) (bool, error) {
	runner.report(ProgressEvent{Phase: ProgressPhaseIteration, State: ProgressStart})
	if err := runner.timer.run(&runner.record.TimeMS.CandidateReset, func() error {
		return runCandidateLifecycle(
			runner.dependencies.Candidate,
			&runner.record.LifecyclePhase,
			func(phase benchrecord.LifecyclePhase, state string, phaseErr error) {
				runner.report(ProgressEvent{Phase: string(phase), State: state, Err: phaseErr})
			},
		)
	}); err != nil {
		runner.record.Final = finalSummary(runner.lastView)
		return true, runner.bail(benchrecord.TerminalStateLifecycleError, err)
	}

	var view agentreport.View
	var exitCode int
	runner.report(ProgressEvent{Phase: ProgressPhaseCompare, State: ProgressStart})
	if err := runner.timer.run(&runner.record.TimeMS.Compare, func() error {
		var err error
		view, exitCode, err = runner.dependencies.Comparator.Compare(runner.config)
		return err
	}); err != nil {
		runner.record.Final = finalSummary(view)
		runner.report(ProgressEvent{Phase: ProgressPhaseCompare, State: ProgressError, Err: err})
		return true, runner.bail(
			benchrecord.TerminalStateToolError,
			fmt.Errorf("compare: %w", err),
		)
	}
	runner.report(ProgressEvent{
		Phase:        ProgressPhaseCompare,
		State:        ProgressDone,
		Actionable:   len(view.Actionable),
		Converged:    view.Converged,
		StillFailing: view.Counts.StillFailing,
	})
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

	// Keep the result outside the timed closure so rendered output survives adapter errors.
	var fix agentFixResult
	runner.report(ProgressEvent{Phase: ProgressPhaseAgentFix, State: ProgressStart})
	err := runner.timer.run(&runner.record.TimeMS.AgentFix, func() error {
		return runner.withAgentFixHeartbeat(func() error {
			var err error
			fix, err = runAgentFix(
				runner.dependencies.Adapter,
				runner.promptTemplate,
				runner.config.Prompt,
				view,
				runner.config.AdapterMetadata,
				runner.record.Tokens,
				&runner.hasKnownTokens,
				&runner.unknownTokenIterations,
			)
			return err
		})
	})
	runner.record.UnknownTokenIterations = runner.unknownTokenIterations
	if fix.Rendered {
		runner.record.PromptInstructions = append(runner.record.PromptInstructions, fix.Instruction)
		runner.record.RenderedPromptHashes = append(runner.record.RenderedPromptHashes, fix.Hash)
		runner.record.AgentResponses = append(runner.record.AgentResponses, fix.Response)
	}
	if fix.Temperature != nil {
		runner.record.Temperature = *fix.Temperature
	}
	if err != nil {
		runner.report(ProgressEvent{Phase: ProgressPhaseAgentFix, State: ProgressError, Err: err})
		return true, runner.bail(benchrecord.TerminalStateAdapterError, err)
	}
	runner.report(ProgressEvent{Phase: ProgressPhaseAgentFix, State: ProgressDone})
	return false, nil
}

func (runner *iterationRunner) bail(state benchrecord.TerminalState, err error) error {
	runner.record.Tokens = tokenRecord(runner.hasKnownTokens, runner.record.Tokens)
	*runner.record = finish(*runner.record, runner.timer.current(), state)
	runner.report(ProgressEvent{Phase: ProgressPhaseTerminal, State: ProgressError, Terminal: state, Err: err})
	return err
}

func (runner *iterationRunner) complete(state benchrecord.TerminalState) {
	runner.record.Tokens = tokenRecord(runner.hasKnownTokens, runner.record.Tokens)
	*runner.record = finish(*runner.record, runner.timer.current(), state)
	runner.report(ProgressEvent{Phase: ProgressPhaseTerminal, State: ProgressDone, Terminal: state})
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

// lifecyclePhaseObserver is notified at the boundary of each candidate
// lifecycle phase. A nil observer disables narration.
type lifecyclePhaseObserver func(phase benchrecord.LifecyclePhase, state string, err error)

func runCandidateLifecycle(
	candidate Candidate,
	failedPhase *benchrecord.LifecyclePhase,
	observe lifecyclePhaseObserver,
) error {
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
		if observe != nil {
			observe(phase.name, ProgressStart, nil)
		}
		if err := phase.call(); err != nil {
			*failedPhase = phase.name
			wrapped := fmt.Errorf("candidate %s: %w", phase.name, err)
			if observe != nil {
				observe(phase.name, ProgressError, wrapped)
			}
			return wrapped
		}
		if observe != nil {
			observe(phase.name, ProgressDone, nil)
		}
	}
	return nil
}

func runPreflight(
	config Config,
	dependencies Dependencies,
	failedPhase *benchrecord.LifecyclePhase,
	recordedTemperature *float64,
) (benchrecord.TerminalState, error) {
	if err := dependencies.Adapter.Preflight(config.AdapterMetadata); err != nil {
		return benchrecord.TerminalStateAdapterError, fmt.Errorf("preflight adapter: %w", err)
	}
	if reporter, ok := dependencies.Adapter.(EffectiveTemperatureReporter); ok {
		temperature := reporter.EffectiveTemperature()
		if temperature != nil {
			if err := validateTemperature(*temperature); err != nil {
				return benchrecord.TerminalStateAdapterError, fmt.Errorf("preflight adapter: %w", err)
			}
			// The adapter may apply an explicit command-line override that is not
			// visible in the campaign metadata. Its preflight result is the source
			// of truth for the recorded effective value.
			*recordedTemperature = *temperature
		}
	}
	if err := runCandidateLifecycle(dependencies.Candidate, failedPhase, nil); err != nil {
		return benchrecord.TerminalStateLifecycleError, fmt.Errorf("preflight lifecycle: %w", err)
	}
	if err := dependencies.Candidate.Stop(); err != nil {
		*failedPhase = benchrecord.LifecyclePhaseStop
		return benchrecord.TerminalStateLifecycleError, fmt.Errorf("preflight stop: %w", err)
	}
	return "", nil
}

func validateTemperature(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 2 {
		return errors.New("temperature must be between 0 and 2")
	}
	return nil
}

func effectiveTemperature(temperature *float64) float64 {
	if temperature == nil {
		return 0
	}
	return *temperature
}

type promptTemplateData struct {
	Prompt         benchrecord.PromptIdentity
	ComparisonView string
}

func renderPrompt(prompt benchrecord.PromptIdentity, view agentreport.View) (string, error) {
	return renderPromptTemplate(promptTemplate, prompt, view)
}

func renderPromptTemplate(
	selectedTemplate *template.Template,
	prompt benchrecord.PromptIdentity,
	view agentreport.View,
) (string, error) {
	compactView, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	var instruction bytes.Buffer
	if err := selectedTemplate.Execute(&instruction, promptTemplateData{
		Prompt:         prompt,
		ComparisonView: string(compactView),
	}); err != nil {
		return "", fmt.Errorf("render task prompt template: %w", err)
	}
	return instruction.String(), nil
}

func loadPromptTemplate(path string) (*template.Template, string, error) {
	if path == "" {
		return promptTemplate, promptTemplateHash, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("load custom prompt template %q: %w", path, err)
	}
	customPrompt, err := template.New("stbench-prompt").Parse(string(content))
	if err != nil {
		return nil, "", fmt.Errorf("parse custom prompt template %q: %w", path, err)
	}
	if !referencesComparisonView(customPrompt) {
		return nil, "", fmt.Errorf("custom prompt template %q must reference .ComparisonView", path)
	}
	return customPrompt, hashContent(string(content)), nil
}

func referencesComparisonView(prompt *template.Template) bool {
	for _, defined := range prompt.Templates() {
		if defined.Tree != nil && nodeReferencesComparisonView(defined.Tree.Root) {
			return true
		}
	}
	return false
}

func nodeReferencesComparisonView(node parse.Node) bool {
	switch typed := node.(type) {
	case *parse.ListNode:
		if typed == nil {
			return false
		}
		for _, child := range typed.Nodes {
			if nodeReferencesComparisonView(child) {
				return true
			}
		}
	case *parse.ActionNode:
		return nodeReferencesComparisonView(typed.Pipe)
	case *parse.IfNode:
		return branchReferencesComparisonView(typed.Pipe, typed.List, typed.ElseList)
	case *parse.RangeNode:
		return branchReferencesComparisonView(typed.Pipe, typed.List, typed.ElseList)
	case *parse.WithNode:
		return branchReferencesComparisonView(typed.Pipe, typed.List, typed.ElseList)
	case *parse.TemplateNode:
		return typed.Pipe != nil && nodeReferencesComparisonView(typed.Pipe)
	case *parse.PipeNode:
		if typed == nil {
			return false
		}
		for _, command := range typed.Cmds {
			if nodeReferencesComparisonView(command) {
				return true
			}
		}
	case *parse.CommandNode:
		for _, argument := range typed.Args {
			if nodeReferencesComparisonView(argument) {
				return true
			}
		}
	case *parse.FieldNode:
		return len(typed.Ident) > 0 && typed.Ident[0] == "ComparisonView"
	case *parse.ChainNode:
		return len(typed.Field) > 0 && typed.Field[0] == "ComparisonView"
	}
	return false
}

func branchReferencesComparisonView(pipe *parse.PipeNode, list, elseList *parse.ListNode) bool {
	if pipe != nil && nodeReferencesComparisonView(pipe) {
		return true
	}
	if list != nil && nodeReferencesComparisonView(list) {
		return true
	}
	return elseList != nil && nodeReferencesComparisonView(elseList)
}

func hashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

type agentFixResult struct {
	Instruction string
	Hash        string
	Response    string
	Temperature *float64
	Rendered    bool
}

func runAgentFix(
	adapter Adapter,
	promptTemplate *template.Template,
	prompt benchrecord.PromptIdentity,
	view agentreport.View,
	metadata AdapterMetadata,
	tokens *benchrecord.TokenUsage,
	hasKnownTokens *bool,
	unknownTokenIterations *int,
) (agentFixResult, error) {
	instruction, err := renderPromptTemplate(promptTemplate, prompt, view)
	if err != nil {
		return agentFixResult{}, fmt.Errorf("render task prompt: %w", err)
	}
	fix := agentFixResult{
		Instruction: instruction,
		Hash:        hashContent(instruction),
		Rendered:    true,
	}
	result, err := adapter.Fix(instruction, view, metadata)
	if result == nil || result.Tokens == nil {
		(*unknownTokenIterations)++
	} else {
		*hasKnownTokens = true
		tokens.Input += result.Tokens.Input
		tokens.Output += result.Tokens.Output
		tokens.Total += result.Tokens.Total
	}
	if result != nil {
		fix.Response = result.Response
		if result.Temperature != nil {
			if err := validateTemperature(*result.Temperature); err != nil {
				return fix, fmt.Errorf("adapter temperature: %w", err)
			}
		}
		fix.Temperature = result.Temperature
	}
	if err != nil {
		return fix, fmt.Errorf("adapter fix: %w", err)
	}
	return fix, nil
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

func tokenRecord(hasKnownTokens bool, usage *benchrecord.TokenUsage) *benchrecord.TokenUsage {
	if !hasKnownTokens {
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
