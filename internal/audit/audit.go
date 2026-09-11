// Package audit reads, finalizes, and renders the local-model turn audit.
package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stcompare/benchrecord"
)

// SchemaVersion is the version of the benchmark audit artifact.
const SchemaVersion = "1"

// Artifact is the durable local-model audit document.
type Artifact struct {
	SchemaVersion      string                        `json:"schema_version"`
	Run                Run                           `json:"run"`
	Capture            Capture                       `json:"capture"`
	Iterations         []Iteration                   `json:"iterations"`
	SharedContent      map[string]json.RawMessage    `json:"shared_content,omitempty"`
	Activity           benchrecord.ActivitySummary   `json:"activity"`
	Efficiency         benchrecord.EfficiencySummary `json:"efficiency"`
	FileModifications  []FileModification            `json:"file_modifications,omitempty"`
	FinalSource        FinalSource                   `json:"final_source,omitempty"`
	LifecycleChanges   []SourceChange                `json:"lifecycle_changes,omitempty"`
	ComparisonOutcomes []ComparisonOutcome           `json:"comparison_outcomes,omitempty"`
	EditSequences      []EditSequence                `json:"edit_sequences,omitempty"`
	Events             []Event                       `json:"events"`
}

// Run identifies the benchmark run associated with the audit.
type Run struct {
	ID            string `json:"id"`
	Candidate     string `json:"candidate"`
	Baseline      string `json:"baseline"`
	Agent         string `json:"agent"`
	Model         string `json:"model"`
	Effort        string `json:"effort"`
	Hardware      string `json:"hardware"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at,omitempty"`
	TerminalState string `json:"terminal_state,omitempty"`
}

// Capture describes whether the artifact contains a complete run capture.
type Capture struct {
	Enabled             bool   `json:"enabled"`
	Status              string `json:"status"`
	Complete            bool   `json:"complete"`
	Failure             string `json:"failure,omitempty"`
	FinishedAt          string `json:"finished_at,omitempty"`
	RecordingOverheadMS int64  `json:"recording_overhead_ms"`
}

// Iteration identifies an ordered benchmark iteration, its model turns, and
// its activity summary.
type Iteration struct {
	ID         string                        `json:"id"`
	Number     int                           `json:"number"`
	TurnIDs    []string                      `json:"turn_ids"`
	Activity   benchrecord.ActivitySummary   `json:"activity"`
	Efficiency benchrecord.EfficiencySummary `json:"efficiency"`
}

// ContentReference identifies shared message content used by one model input.
type ContentReference struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

// FileModification is one content-changing file operation. Before and After
// retain JSON strings, or null when the file did not exist at that point.
type FileModification struct {
	Sequence           int             `json:"sequence"`
	ID                 string          `json:"id"`
	Path               string          `json:"path"`
	Operation          string          `json:"operation"`
	ToolName           string          `json:"tool_name"`
	RunID              string          `json:"run_id"`
	ModelToolCallID    string          `json:"model_tool_call_id"`
	AdapterOperationID string          `json:"adapter_operation_id"`
	TurnID             string          `json:"turn_id"`
	IterationID        string          `json:"iteration_id"`
	Iteration          int             `json:"iteration"`
	Before             json.RawMessage `json:"before"`
	After              json.RawMessage `json:"after"`
	Diff               string          `json:"diff"`
	Created            bool            `json:"created"`
}

// Event is one chronological model-turn or activity record. Model-turn input
// is reconstructable from its shared-content references, and Returned retains
// its exact JSON value from the adapter boundary.
type Event struct {
	Sequence               int                     `json:"sequence"`
	Type                   string                  `json:"type"`
	RunID                  string                  `json:"run_id"`
	IterationID            string                  `json:"iteration_id"`
	Iteration              int                     `json:"iteration"`
	TurnID                 string                  `json:"turn_id"`
	Status                 string                  `json:"status"`
	StartedAt              string                  `json:"started_at"`
	EndedAt                string                  `json:"ended_at,omitempty"`
	DurationMS             int64                   `json:"duration_ms,omitempty"`
	RecordingOverheadMS    int64                   `json:"recording_overhead_ms,omitempty"`
	Sampling               map[string]any          `json:"sampling,omitempty"`
	Input                  json.RawMessage         `json:"input"`
	InputContentReferences []ContentReference      `json:"input_content_references,omitempty"`
	Returned               json.RawMessage         `json:"returned,omitempty"`
	ReturnedMessages       []json.RawMessage       `json:"returned_messages,omitempty"`
	Tokens                 *benchrecord.TokenUsage `json:"tokens,omitempty"`
	Error                  string                  `json:"error,omitempty"`
	ID                     string                  `json:"id,omitempty"`
	ToolCallID             string                  `json:"tool_call_id,omitempty"`
	ModelToolCallID        string                  `json:"model_tool_call_id,omitempty"`
	ToolName               string                  `json:"tool_name,omitempty"`
	Operation              string                  `json:"operation,omitempty"`
	Provenance             string                  `json:"provenance,omitempty"`
	Arguments              json.RawMessage         `json:"arguments,omitempty"`
	Request                json.RawMessage         `json:"request,omitempty"`
	Result                 json.RawMessage         `json:"result,omitempty"`
	EditAttempt            bool                    `json:"edit_attempt,omitempty"`
	FileModificationIDs    []string                `json:"file_modification_ids,omitempty"`
}

// Read loads and validates a versioned audit artifact.
func Read(path string) (Artifact, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("read audit artifact %q: %w", path, err)
	}
	var document Artifact
	if err := json.Unmarshal(contents, &document); err != nil {
		return Artifact{}, fmt.Errorf("parse audit artifact %q: %w", path, err)
	}
	if document.SchemaVersion != SchemaVersion {
		return Artifact{}, fmt.Errorf(
			"audit artifact %q has schema version %q, want %q",
			path,
			document.SchemaVersion,
			SchemaVersion,
		)
	}
	document.Activity = SummarizeActivity(document)
	document.Efficiency = SummarizeEfficiency(document)
	for index := range document.Iterations {
		document.Iterations[index].Activity = summarizeIterationActivity(document, document.Iterations[index].ID)
		document.Iterations[index].Efficiency = summarizeIterationEfficiency(document, document.Iterations[index].ID)
	}
	return document, nil
}

// SummarizeEfficiency returns server-reported token subtotals, model-turn
// timing, and capture overhead for the requested audit scope. A missing usage
// field remains unknown and never becomes a zero-valued usage record.
func SummarizeEfficiency(document Artifact) benchrecord.EfficiencySummary {
	return summarizeEfficiency(document.Capture, document.Events, "")
}

func summarizeIterationEfficiency(document Artifact, iterationID string) benchrecord.EfficiencySummary {
	return summarizeEfficiency(document.Capture, document.Events, iterationID)
}

func summarizeEfficiency(
	capture Capture,
	events []Event,
	iterationID string,
) benchrecord.EfficiencySummary {
	recordingOverheadMS := int64(0)
	if iterationID == "" {
		recordingOverheadMS = capture.RecordingOverheadMS
	}
	summary := benchrecord.EfficiencySummary{
		Status:              efficiencyStatus(capture, events, iterationID),
		TokenStatus:         benchrecord.TokenStatusNotReported,
		RecordingOverheadMS: recordingOverheadMS,
	}
	for _, event := range events {
		if !eventMatchesIteration(event, iterationID) {
			continue
		}
		if event.Type == "model_turn" {
			addTurnEfficiency(&summary, event)
		}
		if iterationID != "" {
			summary.RecordingOverheadMS += event.RecordingOverheadMS
		}
	}
	setTokenStatus(&summary)
	return summary
}

func efficiencyStatus(capture Capture, events []Event, iterationID string) benchrecord.EfficiencyStatus {
	turns := 0
	for _, event := range events {
		if event.Type == "model_turn" && eventMatchesIteration(event, iterationID) {
			turns++
			if EventIsIncomplete(event) || event.Status == "" {
				return benchrecord.EfficiencyStatusPartial
			}
		}
	}
	if turns == 0 {
		return benchrecord.EfficiencyStatusNotReported
	}
	if !capture.Enabled || capture.Status != "complete" || !capture.Complete {
		return benchrecord.EfficiencyStatusPartial
	}
	return benchrecord.EfficiencyStatusComplete
}

func addTurnEfficiency(summary *benchrecord.EfficiencySummary, event Event) {
	summary.Turns++
	switch event.Status {
	case "completed":
		summary.CompletedTurns++
	case "failed":
		summary.FailedTurns++
	default:
		summary.IncompleteTurns++
	}
	if event.Tokens == nil {
		summary.UnknownTokenTurns++
	} else {
		summary.KnownTokenTurns++
		if summary.Tokens == nil {
			summary.Tokens = &benchrecord.TokenUsage{}
		}
		summary.Tokens.Input += event.Tokens.Input
		summary.Tokens.Output += event.Tokens.Output
		summary.Tokens.Total += event.Tokens.Total
	}
	if event.EndedAt != "" || event.Status == "completed" || event.Status == "failed" {
		summary.MeasuredInferenceTurns++
		summary.InferenceMS += event.DurationMS
		return
	}
	summary.UnknownInferenceTurns++
}

func setTokenStatus(summary *benchrecord.EfficiencySummary) {
	if summary.Turns == 0 {
		return
	}
	if summary.UnknownTokenTurns == 0 {
		summary.TokenStatus = benchrecord.TokenStatusComplete
		return
	}
	if summary.KnownTokenTurns > 0 {
		summary.TokenStatus = benchrecord.TokenStatusPartial
		return
	}
	summary.TokenStatus = benchrecord.TokenStatusUnknown
}

// SummarizeActivity returns model-tool, adapter, and file-edit activity in an
// audit. The status remains partial when the capture cannot establish a
// complete zero, so absent evidence is not mistaken for no activity.
func SummarizeActivity(document Artifact) benchrecord.ActivitySummary {
	if !activityEvidenceReported(document) {
		return notReportedActivity()
	}
	return summarizeEvents(document.Capture, document.Events, document.FileModifications, "")
}

func summarizeIterationActivity(document Artifact, iterationID string) benchrecord.ActivitySummary {
	if !activityEvidenceReported(document) {
		return notReportedActivity()
	}
	return summarizeEvents(document.Capture, document.Events, document.FileModifications, iterationID)
}

func activityEvidenceReported(document Artifact) bool {
	return document.Capture.Enabled && (hasActivityEvents(document.Events) || len(document.FileModifications) > 0)
}

func hasActivityEvents(events []Event) bool {
	for _, event := range events {
		if event.Type == "model_tool_call" || event.Type == "adapter_operation" {
			return true
		}
	}
	return false
}

func notReportedActivity() benchrecord.ActivitySummary {
	return benchrecord.ActivitySummary{Status: benchrecord.ActivityStatusNotReported}
}

func summarizeEvents(
	capture Capture,
	events []Event,
	modifications []FileModification,
	iterationID string,
) benchrecord.ActivitySummary {
	summary := benchrecord.ActivitySummary{Status: activityStatus(capture, events)}
	for _, event := range events {
		if !eventMatchesIteration(event, iterationID) {
			continue
		}
		addEventActivity(&summary, event)
	}
	summary.FileModifications = countFileModifications(modifications, iterationID)
	return summary
}

func eventMatchesIteration(event Event, iterationID string) bool {
	return iterationID == "" || event.IterationID == iterationID
}

func addEventActivity(summary *benchrecord.ActivitySummary, event Event) {
	switch event.Type {
	case "model_tool_call":
		addActivityCount(&summary.ModelToolCalls, event)
		if event.EditAttempt {
			summary.EditAttempts++
		}
	case "adapter_operation":
		addActivityCount(&summary.AdapterOperations, event)
		if event.EditAttempt && event.ModelToolCallID == "" {
			summary.EditAttempts++
		}
	}
}

func countFileModifications(modifications []FileModification, iterationID string) int {
	count := 0
	for _, modification := range modifications {
		if iterationID != "" && modification.IterationID != iterationID {
			continue
		}
		count++
	}
	return count
}

func activityStatus(capture Capture, events []Event) benchrecord.ActivityStatus {
	if !capture.Enabled {
		return benchrecord.ActivityStatusNotReported
	}
	if capture.Status != "complete" || !capture.Complete {
		return benchrecord.ActivityStatusPartial
	}
	for _, event := range events {
		if EventIsIncomplete(event) {
			return benchrecord.ActivityStatusPartial
		}
	}
	return benchrecord.ActivityStatusComplete
}

func addActivityCount(counts *benchrecord.ActivityCounts, event Event) {
	counts.Count++
	switch event.Status {
	case "completed":
		counts.Completed++
		counts.DurationMS += event.DurationMS
	case "failed":
		counts.Failed++
	default:
		counts.Incomplete++
	}
}

// EventIsIncomplete reports whether an audit event lacks a terminal outcome.
func EventIsIncomplete(event Event) bool {
	if event.Type == "model_turn" {
		return event.Status != "completed"
	}
	if event.Type != "model_tool_call" && event.Type != "adapter_operation" {
		return false
	}
	return event.Status != "completed" && event.Status != "failed"
}

// Finalize marks an existing audit with the benchmark's terminal state. The
// update preserves raw event JSON so exact model inputs and responses remain
// reconstructable after finalization.
func Finalize(path string, terminalState string, endedAt time.Time, partial bool, failure string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read audit artifact for finalization: %w", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse audit artifact for finalization: %w", err)
	}

	capture, err := rawObject(document, "capture")
	if err != nil {
		return fmt.Errorf("finalize audit capture: %w", err)
	}
	captureStatus := "complete"
	if partial {
		captureStatus = "partial"
	}
	capture["status"] = rawString(captureStatus)
	capture["complete"] = rawBool(!partial)
	capture["finished_at"] = rawString(endedAt.UTC().Format(time.RFC3339Nano))
	if failure != "" {
		capture["failure"] = rawString(failure)
	}
	document["capture"] = mustMarshal(capture)

	run, err := rawObject(document, "run")
	if err != nil {
		return fmt.Errorf("finalize audit run: %w", err)
	}
	run["ended_at"] = rawString(endedAt.UTC().Format(time.RFC3339Nano))
	run["terminal_state"] = rawString(terminalState)
	document["run"] = mustMarshal(run)

	if err := refreshRawActivity(document); err != nil {
		return fmt.Errorf("finalize audit activity: %w", err)
	}
	if err := writeAtomically(path, mustMarshal(document)); err != nil {
		return fmt.Errorf("write finalized audit artifact: %w", err)
	}
	return nil
}

func refreshRawActivity(document map[string]json.RawMessage) error {
	var artifact Artifact
	if err := json.Unmarshal(mustMarshal(document), &artifact); err != nil {
		return fmt.Errorf("parse activity events: %w", err)
	}
	document["activity"] = mustMarshal(SummarizeActivity(artifact))
	document["efficiency"] = mustMarshal(SummarizeEfficiency(artifact))

	rawIterations, ok := document["iterations"]
	if !ok {
		return nil
	}
	var iterations []json.RawMessage
	if err := json.Unmarshal(rawIterations, &iterations); err != nil {
		return fmt.Errorf("parse activity iterations: %w", err)
	}
	for index, rawIteration := range iterations {
		var iteration map[string]json.RawMessage
		if err := json.Unmarshal(rawIteration, &iteration); err != nil {
			return fmt.Errorf("parse activity iteration %d: %w", index, err)
		}
		var iterationID string
		if rawID, exists := iteration["id"]; exists {
			if err := json.Unmarshal(rawID, &iterationID); err != nil {
				return fmt.Errorf("parse activity iteration %d identity: %w", index, err)
			}
		}
		iterationActivity := summarizeIterationActivity(artifact, iterationID)
		iteration["activity"] = mustMarshal(iterationActivity)
		iteration["efficiency"] = mustMarshal(summarizeIterationEfficiency(artifact, iterationID))
		iterations[index] = mustMarshal(iteration)
	}
	document["iterations"] = mustMarshal(iterations)
	return nil
}

// Build reads an audit artifact and writes its chronological HTML report.
func Build(auditPath, outputPath string) error {
	document, err := Read(auditPath)
	if err != nil {
		return err
	}
	html, err := Render(document)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create audit report directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(html), 0o644); err != nil {
		return fmt.Errorf("write audit report %q: %w", outputPath, err)
	}
	return nil
}

// Render renders the chronological audit without requiring comparison output.
func Render(document Artifact) (string, error) {
	status := captureStatus(document)
	efficiency := SummarizeEfficiency(document)
	view := pageView{
		SchemaVersion:       document.SchemaVersion,
		Run:                 document.Run,
		Status:              status,
		StatusClass:         strings.ReplaceAll(status, " ", "-"),
		Partial:             auditIsPartial(document),
		CaptureFailure:      document.Capture.Failure,
		Activity:            SummarizeActivity(document),
		ActivityReported:    activityEvidenceReported(document),
		Efficiency:          efficiency,
		EfficiencyReported:  efficiency.Status != benchrecord.EfficiencyStatusNotReported,
		CaptureEnabled:      document.Capture.Enabled,
		Iterations:          make([]iterationView, 0, len(document.Iterations)),
		FileHistories:       fileHistories(document.FileModifications),
		FinalSourceReported: document.FinalSource.Status != "",
		FinalSource:         newFinalSourceView(document.FinalSource),
		LifecycleChanges:    sourceChangeViews(document.LifecycleChanges),
		ComparisonOutcomes:  comparisonOutcomeViews(document.ComparisonOutcomes),
		EditSequences:       editSequenceViews(document.EditSequences, document.ComparisonOutcomes),
	}
	for _, iteration := range document.Iterations {
		view.Iterations = append(view.Iterations, iterationView{
			ID:               iteration.ID,
			Number:           iteration.Number,
			Activity:         summarizeIterationActivity(document, iteration.ID),
			Efficiency:       summarizeIterationEfficiency(document, iteration.ID),
			ActivityReported: activityEvidenceReported(document),
			Events:           []eventView{},
		})
	}
	for _, event := range document.Events {
		eventView, err := newEventView(event, document.SharedContent)
		if err != nil {
			return "", err
		}
		iterationIndex := view.ensureIteration(event.IterationID, event.Iteration)
		view.Iterations[iterationIndex].Events = append(view.Iterations[iterationIndex].Events, eventView)
	}
	for index := range view.Iterations {
		view.Iterations[index].Activity = summarizeIterationActivity(document, view.Iterations[index].ID)
		view.Iterations[index].Efficiency = summarizeIterationEfficiency(document, view.Iterations[index].ID)
	}
	return executeTemplate(view)
}

func (view *pageView) ensureIteration(iterationID string, number int) int {
	for index, iteration := range view.Iterations {
		if iteration.ID == iterationID {
			return index
		}
	}
	view.Iterations = append(view.Iterations, iterationView{
		ID:               iterationID,
		Number:           number,
		ActivityReported: view.ActivityReported,
		Events:           []eventView{},
	})
	return len(view.Iterations) - 1
}

type pageView struct {
	SchemaVersion       string
	Run                 Run
	Status              string
	StatusClass         string
	Partial             bool
	CaptureFailure      string
	Activity            benchrecord.ActivitySummary
	ActivityReported    bool
	Efficiency          benchrecord.EfficiencySummary
	EfficiencyReported  bool
	CaptureEnabled      bool
	Iterations          []iterationView
	FileHistories       []fileHistoryView
	FinalSourceReported bool
	FinalSource         finalSourceView
	LifecycleChanges    []sourceChangeView
	ComparisonOutcomes  []comparisonOutcomeView
	EditSequences       []editSequenceView
}

type iterationView struct {
	ID               string
	Number           int
	Activity         benchrecord.ActivitySummary
	Efficiency       benchrecord.EfficiencySummary
	ActivityReported bool
	Events           []eventView
}

type eventView struct {
	Sequence            int
	Type                string
	Iteration           int
	IterationID         string
	TurnID              string
	Status              string
	StartedAt           string
	EndedAt             string
	DurationMS          int64
	DurationReported    bool
	RecordingOverheadMS int64
	Tokens              *benchrecord.TokenUsage
	TokenStatus         benchrecord.TokenStatus
	Sampling            *payloadView
	Input               *payloadView
	Returned            *payloadView
	ReturnedMessages    []messageView
	Error               string
	Partial             bool
	ModelToolCall       bool
	AdapterOperation    bool
	ID                  string
	ToolCallID          string
	ModelToolCallID     string
	ToolName            string
	Operation           string
	Provenance          string
	Arguments           *payloadView
	Request             *payloadView
	Result              *payloadView
}

type payloadView struct {
	Label string
	JSON  string
}

type messageView struct {
	Payload payloadView
	Content string
	HasText bool
}

type fileHistoryView struct {
	Path          string
	Modifications []modificationView
}

type modificationView struct {
	Sequence           int
	ID                 string
	Path               string
	Operation          string
	ToolName           string
	ModelToolCallID    string
	AdapterOperationID string
	TurnID             string
	IterationID        string
	Iteration          int
	Before             string
	After              string
	BeforeEmpty        bool
	AfterEmpty         bool
	Diff               string
	Created            bool
}

type finalSourceView struct {
	Status            string
	StartingStatus    string
	FinalStatus       string
	FilesChangedAtEnd int
	Diffs             []sourceChangeView
}

type sourceChangeView struct {
	Sequence    int
	ID          string
	Path        string
	Phase       string
	Origin      string
	Before      string
	After       string
	BeforeEmpty bool
	AfterEmpty  bool
	Diff        string
	Created     bool
	Deleted     bool
}

type comparisonOutcomeView struct {
	Sequence    int
	ID          string
	Iteration   int
	IterationID string
	Status      string
	StartedAt   string
	EndedAt     string
	DurationMS  int64
	ExitCode    int
	View        string
	Error       string
}

type editSequenceView struct {
	Sequence               int
	ID                     string
	Iteration              int
	IterationID            string
	ProblemInput           string
	ComparisonBeforeID     string
	EvaluationStatus       string
	SubsequentComparisonID string
	SubsequentComparison   *comparisonOutcomeView
}

func newFinalSourceView(source FinalSource) finalSourceView {
	return finalSourceView{
		Status:            source.Status,
		StartingStatus:    source.Starting.Status,
		FinalStatus:       source.Final.Status,
		FilesChangedAtEnd: source.FilesChangedAtEnd,
		Diffs:             sourceChangeViews(source.Diffs),
	}
}

func sourceChangeViews(changes []SourceChange) []sourceChangeView {
	return mapViews(changes, newSourceChangeView)
}

func newSourceChangeView(change SourceChange) sourceChangeView {
	origin := change.Origin
	if origin == "" {
		origin = ChangeOriginUnattributed
	}
	return sourceChangeView{
		Sequence:    change.Sequence,
		ID:          change.ID,
		Path:        change.Path,
		Phase:       change.Phase,
		Origin:      origin,
		Before:      sourceContent(change.Before),
		After:       sourceContent(change.After),
		BeforeEmpty: isEmptySourceContent(change.Before),
		AfterEmpty:  isEmptySourceContent(change.After),
		Diff:        focusedSourceDiff(change.Path, change.Before, change.After, change.Diff),
		Created:     change.Created,
		Deleted:     change.Deleted,
	}
}

func sourceContent(content *string) string {
	if content == nil {
		return "File did not exist"
	}
	return *content
}

func comparisonOutcomeViews(outcomes []ComparisonOutcome) []comparisonOutcomeView {
	return mapViews(outcomes, newComparisonOutcomeView)
}

func newComparisonOutcomeView(outcome ComparisonOutcome) comparisonOutcomeView {
	return comparisonOutcomeView{
		Sequence:    outcome.Sequence,
		ID:          outcome.ID,
		Iteration:   outcome.Iteration,
		IterationID: outcome.IterationID,
		Status:      outcome.Status,
		StartedAt:   outcome.StartedAt,
		EndedAt:     outcome.EndedAt,
		DurationMS:  outcome.DurationMS,
		ExitCode:    outcome.ExitCode,
		View:        formatJSON(outcome.View),
		Error:       outcome.Error,
	}
}

func editSequenceViews(sequences []EditSequence, outcomes []ComparisonOutcome) []editSequenceView {
	byID := make(map[string]comparisonOutcomeView, len(outcomes))
	for _, outcome := range mapViews(outcomes, newComparisonOutcomeView) {
		byID[outcome.ID] = outcome
	}
	return mapViews(sequences, func(sequence EditSequence) editSequenceView {
		view := editSequenceView{
			Sequence:               sequence.Sequence,
			ID:                     sequence.ID,
			Iteration:              sequence.Iteration,
			IterationID:            sequence.IterationID,
			ProblemInput:           sequence.ProblemInput,
			ComparisonBeforeID:     sequence.ComparisonBeforeID,
			EvaluationStatus:       sequence.EvaluationStatus,
			SubsequentComparisonID: sequence.SubsequentComparisonID,
		}
		if outcome, ok := byID[sequence.SubsequentComparisonID]; ok {
			view.SubsequentComparison = &outcome
		}
		return view
	})
}

func mapViews[Source any, View any](values []Source, newView func(Source) View) []View {
	views := make([]View, 0, len(values))
	for _, value := range values {
		views = append(views, newView(value))
	}
	return views
}

func fileHistories(modifications []FileModification) []fileHistoryView {
	ordered := append([]FileModification(nil), modifications...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].Sequence < ordered[right].Sequence
	})
	histories := make([]fileHistoryView, 0)
	indices := make(map[string]int)
	for _, modification := range ordered {
		index, exists := indices[modification.Path]
		if !exists {
			index = len(histories)
			indices[modification.Path] = index
			histories = append(histories, fileHistoryView{Path: modification.Path})
		}
		histories[index].Modifications = append(
			histories[index].Modifications,
			newModificationView(modification),
		)
	}
	return histories
}

func newModificationView(modification FileModification) modificationView {
	before, _ := rawFileContent(modification.Before)
	after, _ := rawFileContent(modification.After)
	return modificationView{
		Sequence:           modification.Sequence,
		ID:                 modification.ID,
		Path:               modification.Path,
		Operation:          modification.Operation,
		ToolName:           modification.ToolName,
		ModelToolCallID:    modification.ModelToolCallID,
		AdapterOperationID: modification.AdapterOperationID,
		TurnID:             modification.TurnID,
		IterationID:        modification.IterationID,
		Iteration:          modification.Iteration,
		Before:             formatFileContent(modification.Before),
		After:              formatFileContent(modification.After),
		BeforeEmpty:        isEmptySourceContent(before),
		AfterEmpty:         isEmptySourceContent(after),
		Diff:               focusedSourceDiff(modification.Path, before, after, modification.Diff),
		Created:            modification.Created,
	}
}

func focusedSourceDiff(path string, before, after *string, captured string) string {
	focused := unifiedSourceDiff(path, before, after)
	if focused == "" {
		// Keep the captured diff when source pointers cannot establish a transition.
		return captured
	}
	return focused
}

func isEmptySourceContent(content *string) bool {
	return content != nil && *content == ""
}

func formatFileContent(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "File did not exist"
	}
	var content string
	if err := json.Unmarshal(raw, &content); err == nil {
		return content
	}
	return formatJSON(raw)
}

func newEventView(event Event, sharedContent map[string]json.RawMessage) (eventView, error) {
	input, err := reconstructInput(event.Input, event.InputContentReferences, sharedContent)
	if err != nil {
		return eventView{}, err
	}
	argumentsLabel := "Tool arguments (JSON)"
	resultLabel := "Tool result (JSON)"
	if event.Type == "adapter_operation" {
		argumentsLabel = "Adapter Operation arguments (JSON)"
		resultLabel = "Adapter Operation result (JSON)"
	}
	view := eventView{
		Sequence:            event.Sequence,
		Type:                event.Type,
		Iteration:           event.Iteration,
		IterationID:         event.IterationID,
		TurnID:              event.TurnID,
		Status:              event.Status,
		StartedAt:           event.StartedAt,
		EndedAt:             event.EndedAt,
		DurationMS:          event.DurationMS,
		DurationReported:    event.EndedAt != "" || event.Status == "completed" || event.Status == "failed",
		RecordingOverheadMS: event.RecordingOverheadMS,
		Tokens:              event.Tokens,
		TokenStatus:         tokenStatus(event.Tokens),
		Sampling:            newSamplingPayload(event.Sampling),
		Input:               newRequiredJSONPayload("Exact model input (JSON)", input),
		Returned:            newJSONPayload("Returned model response (JSON)", event.Returned),
		Error:               event.Error,
		Partial:             EventIsIncomplete(event),
		ModelToolCall:       event.Type == "model_tool_call",
		AdapterOperation:    event.Type == "adapter_operation",
		ID:                  event.ID,
		ToolCallID:          event.ToolCallID,
		ModelToolCallID:     event.ModelToolCallID,
		ToolName:            event.ToolName,
		Operation:           event.Operation,
		Provenance:          event.Provenance,
		Arguments:           newJSONPayload(argumentsLabel, event.Arguments),
		Request:             newJSONPayload("Tool request (JSON)", event.Request),
		Result:              newJSONPayload(resultLabel, event.Result),
	}
	for index, rawMessage := range event.ReturnedMessages {
		messagePayload := newRequiredJSONPayload(
			fmt.Sprintf("Returned model message %d (JSON)", index+1),
			rawMessage,
		)
		message := messageView{Payload: *messagePayload}
		var decoded map[string]any
		if err := json.Unmarshal(rawMessage, &decoded); err == nil {
			if content, ok := decoded["content"].(string); ok && strings.TrimSpace(content) != "" {
				message.Content = content
				message.HasText = true
			}
		}
		view.ReturnedMessages = append(view.ReturnedMessages, message)
	}
	return view, nil
}

func newSamplingPayload(settings map[string]any) *payloadView {
	if settings == nil {
		return nil
	}
	return &payloadView{
		Label: "Effective sampling settings (JSON)",
		JSON:  formatJSON(mustMarshal(settings)),
	}
}

func newJSONPayload(label string, raw json.RawMessage) *payloadView {
	if !hasJSONPayload(raw) {
		return nil
	}
	return &payloadView{Label: label, JSON: formatJSON(raw)}
}

func newRequiredJSONPayload(label string, raw json.RawMessage) *payloadView {
	return &payloadView{Label: label, JSON: formatJSON(raw)}
}

func hasJSONPayload(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	hasContent := len(trimmed) > 0
	isNull := bytes.Equal(trimmed, []byte("null"))
	return hasContent && !isNull
}

func tokenStatus(tokens *benchrecord.TokenUsage) benchrecord.TokenStatus {
	if tokens == nil {
		return benchrecord.TokenStatusUnknown
	}
	return benchrecord.TokenStatusComplete
}

func reconstructInput(
	input json.RawMessage,
	references []ContentReference,
	sharedContent map[string]json.RawMessage,
) (json.RawMessage, error) {
	if len(references) == 0 {
		return input, nil
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode model input for content references: %w", err)
	}
	for _, reference := range references {
		rawContent, ok := sharedContent[reference.ID]
		if !ok {
			return nil, fmt.Errorf("shared model content %q is missing", reference.ID)
		}
		var content any
		contentDecoder := json.NewDecoder(bytes.NewReader(rawContent))
		contentDecoder.UseNumber()
		if err := contentDecoder.Decode(&content); err != nil {
			return nil, fmt.Errorf("decode shared model content %q: %w", reference.ID, err)
		}
		if err := setJSONPointer(&document, reference.Path, content); err != nil {
			return nil, err
		}
	}
	reconstructed, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode reconstructed model input: %w", err)
	}
	return reconstructed, nil
}

func setJSONPointer(document *any, pointer string, value any) error {
	if len(pointer) == 0 || pointer[0] != '/' {
		return fmt.Errorf("invalid model input JSON pointer %q", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	for index := range parts {
		parts[index] = strings.ReplaceAll(parts[index], "~1", "/")
		parts[index] = strings.ReplaceAll(parts[index], "~0", "~")
	}
	current := *document
	for _, part := range parts[:len(parts)-1] {
		next, err := jsonPointerChild(current, part)
		if err != nil {
			return fmt.Errorf("resolve model input JSON pointer %q: %w", pointer, err)
		}
		current = next
	}
	if err := replaceJSONPointerChild(current, parts[len(parts)-1], value); err != nil {
		return fmt.Errorf("resolve model input JSON pointer %q: %w", pointer, err)
	}
	return nil
}

func jsonPointerChild(value any, part string) (any, error) {
	object, ok := value.(map[string]any)
	if ok {
		child, exists := object[part]
		if !exists {
			return nil, fmt.Errorf("field %q does not exist", part)
		}
		return child, nil
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("value is not an object or array")
	}
	index, err := parseJSONPointerIndex(part, len(array))
	if err != nil {
		return nil, err
	}
	return array[index], nil
}

func replaceJSONPointerChild(value any, part string, replacement any) error {
	if object, ok := value.(map[string]any); ok {
		if _, exists := object[part]; !exists {
			return fmt.Errorf("field %q does not exist", part)
		}
		object[part] = replacement
		return nil
	}
	array, ok := value.([]any)
	if !ok {
		return fmt.Errorf("value is not an object or array")
	}
	index, err := parseJSONPointerIndex(part, len(array))
	if err != nil {
		return err
	}
	array[index] = replacement
	return nil
}

func parseJSONPointerIndex(value string, length int) (int, error) {
	var index int
	if _, err := fmt.Sscanf(value, "%d", &index); err != nil || index < 0 || index >= length {
		return 0, fmt.Errorf("array index %q is invalid", value)
	}
	return index, nil
}

func captureStatus(document Artifact) string {
	if !document.Capture.Enabled {
		return "not reported"
	}
	if auditIsPartial(document) {
		return "partial"
	}
	return "complete"
}

func auditIsPartial(document Artifact) bool {
	if !document.Capture.Enabled {
		return false
	}
	if document.Capture.Status != "complete" || !document.Capture.Complete {
		return true
	}
	for _, event := range document.Events {
		if EventIsIncomplete(event) {
			return true
		}
	}
	if document.FinalSource.Status == SourceStatusPartial || document.FinalSource.Status == SourceStatusUnavailable {
		return true
	}
	return false
}

func executeTemplate(view pageView) (string, error) {
	var output bytes.Buffer
	if err := auditTemplate.Execute(&output, view); err != nil {
		return "", fmt.Errorf("render audit report: %w", err)
	}
	return output.String(), nil
}

func formatJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "not captured"
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return string(raw)
	}
	return indented.String()
}

func rawObject(document map[string]json.RawMessage, name string) (map[string]json.RawMessage, error) {
	raw, ok := document[name]
	if !ok {
		return nil, fmt.Errorf("missing %s object", name)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func rawString(value string) json.RawMessage {
	return mustMarshal(value)
}

func rawBool(value bool) json.RawMessage {
	return mustMarshal(value)
}

func mustMarshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal audit value: %v", err))
	}
	return encoded
}

func writeAtomically(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".audit-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

var auditTemplate = template.Must(template.New("audit").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Model-turn audit</title>
<style>
body { font: 16px/1.5 system-ui, sans-serif; margin: 2rem auto; max-width: 1100px; padding: 0 1rem; color: #202124; }
header, section, article { margin-bottom: 1.5rem; }
header { border-bottom: 1px solid #d0d7de; padding-bottom: 1rem; }
.status { display: inline-block; border-radius: .25rem; padding: .15rem .45rem; font-weight: 700; text-transform: uppercase; }
.status.partial { background: #fff0c2; color: #7a4b00; }
.status.complete { background: #d8f3dc; color: #1b5e20; }
.status.not-reported { background: #edf0f2; color: #57606a; }
.identity, .turn-meta { display: grid; gap: .4rem 1.5rem; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
.identity span, .turn-meta span { color: #57606a; display: block; font-size: .85rem; }
.iteration { border-top: 3px solid #0969da; padding-top: .5rem; }
.turn { border: 1px solid #d0d7de; border-radius: .4rem; padding: 1rem; }
.turn.partial { border-color: #bf8700; }
.activity-entry { border: 1px solid #d0d7de; border-radius: .4rem; margin: 1rem 0; padding: .75rem 1rem; }
.activity-entry.partial { border-color: #bf8700; }
.activity-entry summary { cursor: pointer; font-weight: 700; }
.payload { border: 1px solid #d0d7de; border-radius: .3rem; margin: .75rem 0; padding: .5rem .75rem; }
.payload summary { cursor: pointer; font-weight: 700; }
.file-history { border: 1px solid #d0d7de; border-radius: .4rem; margin: 1rem 0; padding: .75rem 1rem; }
.file-history summary { cursor: pointer; font-weight: 700; }
.source-change { border: 1px solid #d0d7de; border-radius: .4rem; margin: 1rem 0; padding: .75rem 1rem; }
.source-change summary { cursor: pointer; font-weight: 700; }
.source-view { margin: .75rem 0; }
.source-view summary { cursor: pointer; font-weight: 700; }
.source-status.partial, .source-status.unavailable { color: #7a4b00; font-weight: 700; }
.activity-summary, .iteration-activity { border: 1px solid #d0d7de; border-radius: .4rem; padding: 1rem; }
.activity-counts { display: grid; gap: .75rem; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); }
.activity-count { background: #f6f8fa; border-radius: .3rem; padding: .65rem; }
.activity-count span, .activity-count small { color: #57606a; display: block; }
.activity-count strong { display: block; font-size: 1.3rem; }
pre { background: #f6f8fa; border-radius: .3rem; overflow-x: auto; padding: .75rem; white-space: pre-wrap; word-break: break-word; }
.rationale { border-left: 4px solid #8250df; padding-left: .75rem; }
.label { color: #8250df; font-weight: 700; }
.empty { color: #57606a; }
</style>
</head>
<body>
<header>
<h1>Model-turn audit</h1>
<p>Evidence schema {{.SchemaVersion}} — <span class="status {{.StatusClass}}">{{.Status}}</span></p>
{{with .CaptureFailure}}<p>Audit capture failure: {{.}}</p>{{end}}
<div class="identity">
<div><span>Run</span><strong>{{.Run.ID}}</strong></div>
<div><span>Candidate</span><strong>{{.Run.Candidate}}</strong></div>
<div><span>Baseline</span><strong>{{.Run.Baseline}}</strong></div>
<div><span>Agent / model</span><strong>{{.Run.Agent}} / {{.Run.Model}}</strong></div>
{{with .Run.TerminalState}}<div><span>Terminal state</span><strong>{{.}}</strong></div>{{end}}
<div><span>Started</span><strong>{{.Run.StartedAt}}</strong></div>
{{with .Run.EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
</div>
{{if .Partial}}<p class="empty">This audit is partial. Unfinished activity and incomplete capture are shown as evidence, not as zero activity.</p>{{end}}
</header>
{{if .ActivityReported}}
<section class="activity-summary">
<h2>Activity summary <small>({{.Activity.Status}} evidence)</small></h2>
<div class="activity-counts">
<div class="activity-count"><span>Model Tool Calls</span><strong>{{.Activity.ModelToolCalls.Count}}</strong><small>{{.Activity.ModelToolCalls.Completed}} completed · {{.Activity.ModelToolCalls.Failed}} failed · {{.Activity.ModelToolCalls.Incomplete}} incomplete</small><small>{{.Activity.ModelToolCalls.DurationMS}} ms execution time</small></div>
<div class="activity-count"><span>Adapter Operations</span><strong>{{.Activity.AdapterOperations.Count}}</strong><small>{{.Activity.AdapterOperations.Completed}} completed · {{.Activity.AdapterOperations.Failed}} failed · {{.Activity.AdapterOperations.Incomplete}} incomplete</small><small>{{.Activity.AdapterOperations.DurationMS}} ms execution time</small></div>
<div class="activity-count"><span>Edit Attempts</span><strong>{{.Activity.EditAttempts}}</strong></div>
<div class="activity-count"><span>File Modifications</span><strong>{{.Activity.FileModifications}}</strong></div>
</div>
</section>
{{else if .CaptureEnabled}}
<section class="activity-summary">
<h2>Activity summary <small>(not reported)</small></h2>
<p class="empty">Activity evidence: not reported.</p>
</section>
{{end}}
{{if .EfficiencyReported}}
<section class="activity-summary">
<h2>Efficiency summary <small>({{.Efficiency.Status}} evidence)</small></h2>
<div class="activity-counts">
<div class="activity-count"><span>Model turns</span><strong>{{.Efficiency.Turns}}</strong><small>{{.Efficiency.CompletedTurns}} completed · {{.Efficiency.FailedTurns}} failed · {{.Efficiency.IncompleteTurns}} incomplete</small></div>
<div class="activity-count"><span>Inference time</span><strong>{{.Efficiency.InferenceMS}} ms</strong><small>{{.Efficiency.MeasuredInferenceTurns}} measured · {{.Efficiency.UnknownInferenceTurns}} unknown</small></div>
<div class="activity-count"><span>Audit-recording overhead</span><strong>{{.Efficiency.RecordingOverheadMS}} ms</strong><small>excluded from inference time</small></div>
<div class="activity-count"><span>Token evidence</span><strong>{{.Efficiency.TokenStatus}}</strong><small>{{.Efficiency.KnownTokenTurns}} known turns · {{.Efficiency.UnknownTokenTurns}} unknown turns</small></div>
</div>
{{with .Efficiency.Tokens}}<p>Known token subtotal: {{.Input}} input · {{.Output}} output · {{.Total}} total ({{$.Efficiency.TokenStatus}}).</p>{{else}}<p class="empty">Token usage: unknown; no server-reported usage was captured.</p>{{end}}
<p class="empty">Inference time starts after the request audit record is durably written and ends when the server response or request error is received. Audit-recording overhead covers capture work around that boundary and is not included in inference time. Existing run wall-clock and phase totals still include both.</p>
</section>
{{else if .CaptureEnabled}}
<section class="activity-summary">
<h2>Efficiency summary <small>(not reported)</small></h2>
<p class="empty">Model-turn efficiency evidence: not reported.</p>
</section>
{{end}}
{{if .FinalSourceReported}}
<section class="final-source">
<h2>Final source</h2>
<p>Net source diff from the actual starting source after initial lifecycle preparation to the final source. Git HEAD is not used.</p>
<div class="activity-counts">
<div class="activity-count"><span>Snapshot evidence</span><strong>{{.FinalSource.Status}}</strong><small>starting: {{.FinalSource.StartingStatus}} · final: {{.FinalSource.FinalStatus}}</small></div>
<div class="activity-count"><span>Files Changed at the End</span><strong>{{.FinalSource.FilesChangedAtEnd}}</strong><small>distinct files with different final content</small></div>
</div>
{{if eq .FinalSource.Status "unavailable"}}
<p class="empty">Final source: unavailable. A complete net diff could not be established.</p>
{{else if eq .FinalSource.Status "partial"}}
<p class="empty">Final source snapshot is partial. The displayed diff is partial evidence and is not a complete final diff.</p>
{{end}}
{{if .FinalSource.Diffs}}
<h3>Net final source diff</h3>
{{range .FinalSource.Diffs}}
<article class="source-change">
<details>
<summary>{{.Path}} — origin: {{.Origin}}</summary>
<div class="turn-meta">
<div><span>Diff ID</span><strong>{{.ID}}</strong></div>
{{with .Origin}}<div><span>Origin</span><strong>{{.}}</strong></div>{{end}}
{{if .Created}}<div><span>Change</span><strong>created</strong></div>{{else if .Deleted}}<div><span>Change</span><strong>deleted</strong></div>{{else}}<div><span>Change</span><strong>modified</strong></div>{{end}}
</div>
<h4>Focused diff</h4><pre>{{.Diff}}</pre>
<details class="source-view">
<summary>Before (full source)</summary>
{{if .BeforeEmpty}}<p class="empty source-state">Empty file</p>{{end}}
<pre>{{.Before}}</pre>
</details>
<details class="source-view">
<summary>After (full source)</summary>
{{if .AfterEmpty}}<p class="empty source-state">Empty file</p>{{end}}
<pre>{{.After}}</pre>
</details>
</details>
</article>
{{end}}
{{else if eq .FinalSource.Status "complete"}}
<p class="empty">No net source changes.</p>
{{end}}
</section>
{{end}}
{{if .LifecycleChanges}}
<section class="lifecycle-changes">
<h2>Lifecycle changes</h2>
<p>These source changes were observed around lifecycle commands. They remain visible but are excluded from model edit counts.</p>
{{range .LifecycleChanges}}
<article class="source-change">
<details>
<summary>Lifecycle change {{.Sequence}} — {{.Path}} — {{.Phase}}</summary>
<div class="turn-meta">
<div><span>Change ID</span><strong>{{.ID}}</strong></div>
<div><span>Origin</span><strong>{{.Origin}}</strong></div>
<div><span>Phase</span><strong>{{.Phase}}</strong></div>
</div>
<h4>Focused diff</h4><pre>{{.Diff}}</pre>
<details class="source-view">
<summary>Before (full source)</summary>
{{if .BeforeEmpty}}<p class="empty source-state">Empty file</p>{{end}}
<pre>{{.Before}}</pre>
</details>
<details class="source-view">
<summary>After (full source)</summary>
{{if .AfterEmpty}}<p class="empty source-state">Empty file</p>{{end}}
<pre>{{.After}}</pre>
</details>
</details>
</article>
{{end}}
</section>
{{end}}
{{if .EditSequences}}
<section class="comparison-evidence">
<h2>Model problem input and comparison outcomes</h2>
<p>Each edit sequence shows the problems delivered to the model and the next comparison in chronological order. The report does not infer that an individual edit caused a Problem Outcome.</p>
<p>Fixed is a replay-backed Problem Outcome. It is not a human Fix Quality Assessment, and researcher assessments are not stored here.</p>
{{range .EditSequences}}
<article class="source-change">
<h3>Edit sequence {{.Sequence}} <small>({{.ID}}; iteration {{.Iteration}} — {{.IterationID}})</small></h3>
<div class="turn-meta">
<div><span>Comparison before edit</span><strong>{{.ComparisonBeforeID}}</strong></div>
<div><span>Evaluation status</span><strong>{{.EvaluationStatus}}</strong></div>
{{with .SubsequentComparisonID}}<div><span>Subsequent comparison</span><strong>{{.}}</strong></div>{{end}}
</div>
<h4>Problems delivered to model</h4><pre>{{.ProblemInput}}</pre>
{{with .SubsequentComparison}}
<h4>Subsequent comparison outcome</h4>
<div class="turn-meta">
<div><span>Comparison status</span><strong>{{.Status}}</strong></div>
<div><span>Exit code</span><strong>{{.ExitCode}}</strong></div>
{{with .DurationMS}}<div><span>Duration</span><strong>{{.}} ms</strong></div>{{end}}
</div>
<pre>{{.View}}</pre>
{{with .Error}}<p>Comparison error: {{.}}</p>{{end}}
{{else}}
<p class="empty">Subsequent comparison: not evaluated. The run ended before this edit sequence had a comparison.</p>
{{end}}
</article>
{{end}}
</section>
{{end}}
{{if .ComparisonOutcomes}}
<section class="comparison-outcomes">
<h2>Chronological comparison outcomes</h2>
<p>These are replay evidence records, not causal claims about individual edits.</p>
{{range .ComparisonOutcomes}}
<article class="activity-entry {{if eq .Status "failed"}}partial{{end}}">
<details>
<summary>Comparison {{.ID}} — iteration {{.Iteration}} — {{.Status}}</summary>
<div class="turn-meta">
<div><span>Chronological sequence</span><strong>{{.Sequence}}</strong></div>
<div><span>Exit code</span><strong>{{.ExitCode}}</strong></div>
{{with .StartedAt}}<div><span>Started</span><strong>{{.}}</strong></div>{{end}}
{{with .EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
</div>
<h4>Comparison view</h4><pre>{{.View}}</pre>
{{with .Error}}<p>Comparison error: {{.}}</p>{{end}}
</details>
</article>
{{end}}
</section>
{{end}}
{{if .FileHistories}}
<section class="file-histories">
<h2>File modification history</h2>
{{range .FileHistories}}
<article class="file-history">
<h3>{{.Path}}</h3>
{{range .Modifications}}
<details>
<summary>File Modification {{.Sequence}} — {{if .Created}}created{{else}}modified{{end}}</summary>
<div class="turn-meta">
<div><span>Modification ID</span><strong>{{.ID}}</strong></div>
{{with .Operation}}<div><span>Operation</span><strong>{{.}}</strong></div>{{end}}
{{with .ToolName}}<div><span>Tool</span><strong>{{.}}</strong></div>{{end}}
<div><span>Model turn</span><strong>{{.TurnID}}</strong></div>
<div><span>Iteration</span><strong>{{.Iteration}}</strong></div>
{{with .ModelToolCallID}}<div><span>Model Tool Call</span><strong>{{.}}</strong></div>{{end}}
{{with .AdapterOperationID}}<div><span>Adapter Operation</span><strong>{{.}}</strong></div>{{end}}
</div>
<h4>Focused diff</h4><pre>{{.Diff}}</pre>
<details class="source-view">
<summary>Before (full source)</summary>
{{if .BeforeEmpty}}<p class="empty source-state">Empty file</p>{{end}}
<pre>{{.Before}}</pre>
</details>
<details class="source-view">
<summary>After (full source)</summary>
{{if .AfterEmpty}}<p class="empty source-state">Empty file</p>{{end}}
<pre>{{.After}}</pre>
</details>
</details>
{{end}}
</article>
{{end}}
</section>
{{end}}
{{if .Iterations}}
{{range .Iterations}}
<section class="iteration">
<h2>Iteration {{.Number}} <small>({{.ID}})</small></h2>
{{if .ActivityReported}}
<div class="iteration-activity">
<strong>Iteration activity</strong>
<div class="activity-counts">
<div class="activity-count"><span>Model Tool Calls</span><strong>{{.Activity.ModelToolCalls.Count}}</strong><small>{{.Activity.ModelToolCalls.DurationMS}} ms execution time</small></div>
<div class="activity-count"><span>Adapter Operations</span><strong>{{.Activity.AdapterOperations.Count}}</strong><small>{{.Activity.AdapterOperations.DurationMS}} ms execution time</small></div>
<div class="activity-count"><span>Edit Attempts</span><strong>{{.Activity.EditAttempts}}</strong></div>
<div class="activity-count"><span>File Modifications</span><strong>{{.Activity.FileModifications}}</strong></div>
</div>
</div>
{{end}}
{{if ne .Efficiency.Status "not_reported"}}
<div class="iteration-activity">
<strong>Iteration efficiency <small>({{.Efficiency.Status}} evidence)</small></strong>
<div class="activity-counts">
<div class="activity-count"><span>Model turns</span><strong>{{.Efficiency.Turns}}</strong><small>{{.Efficiency.CompletedTurns}} completed · {{.Efficiency.FailedTurns}} failed · {{.Efficiency.IncompleteTurns}} incomplete</small></div>
<div class="activity-count"><span>Inference time</span><strong>{{.Efficiency.InferenceMS}} ms</strong><small>{{.Efficiency.MeasuredInferenceTurns}} measured</small></div>
<div class="activity-count"><span>Token status</span><strong>{{.Efficiency.TokenStatus}}</strong><small>{{.Efficiency.KnownTokenTurns}} known · {{.Efficiency.UnknownTokenTurns}} unknown</small></div>
</div>
{{with .Efficiency.Tokens}}<p>Known token subtotal: {{.Input}} input · {{.Output}} output · {{.Total}} total.</p>{{end}}
</div>
{{end}}
{{range .Events}}
{{if .ModelToolCall}}
<article class="activity-entry {{if .Partial}}partial{{end}}">
<details>
<summary>Model Tool Call {{.ID}} — {{.ToolName}} — {{.Status}}</summary>
<div class="turn-meta">
<div><span>Chronological sequence</span><strong>{{.Sequence}}</strong></div>
<div><span>Model turn</span><strong>{{.TurnID}}</strong></div>
{{with .ToolCallID}}<div><span>Tool call ID</span><strong>{{.}}</strong></div>{{end}}
{{with .Provenance}}<div><span>Provenance</span><strong>{{.}}</strong></div>{{end}}
<div><span>Started</span><strong>{{.StartedAt}}</strong></div>
{{with .EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
{{if .DurationReported}}<div><span>Execution time</span><strong>{{.DurationMS}} ms</strong></div>{{end}}
</div>
{{template "payload" .Arguments}}
{{template "payload" .Request}}
{{template "payload" .Result}}
{{with .Error}}<p>Tool error: {{.}}</p>{{end}}
{{if .Partial}}<p class="empty">Incomplete Model Tool Call: execution did not produce a terminal result.</p>{{end}}
</details>
</article>
{{else if .AdapterOperation}}
<article class="activity-entry {{if .Partial}}partial{{end}}">
<details>
<summary>Adapter Operation {{.ID}} — {{.Operation}} — {{.Status}}</summary>
<div class="turn-meta">
<div><span>Chronological sequence</span><strong>{{.Sequence}}</strong></div>
<div><span>Model Tool Call</span><strong>{{.ModelToolCallID}}</strong></div>
{{with .ToolName}}<div><span>Tool</span><strong>{{.}}</strong></div>{{end}}
<div><span>Started</span><strong>{{.StartedAt}}</strong></div>
{{with .EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
{{if .DurationReported}}<div><span>Execution time</span><strong>{{.DurationMS}} ms</strong></div>{{end}}
</div>
{{template "payload" .Arguments}}
{{template "payload" .Result}}
{{with .Error}}<p>Operation error: {{.}}</p>{{end}}
{{if .Partial}}<p class="empty">Incomplete Adapter Operation: execution did not produce a terminal result.</p>{{end}}
</details>
</article>
{{else}}
<article class="turn {{if .Partial}}partial{{end}}">
<h3>Model turn {{.TurnID}} — {{.Status}}</h3>
<div class="turn-meta">
<div><span>Chronological sequence</span><strong>{{.Sequence}}</strong></div>
<div><span>Started</span><strong>{{.StartedAt}}</strong></div>
{{with .EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
{{if .DurationReported}}<div><span>Inference time</span><strong>{{.DurationMS}} ms</strong></div>{{end}}
{{with .RecordingOverheadMS}}<div><span>Audit-recording overhead</span><strong>{{.}} ms</strong></div>{{end}}
<div><span>Token usage</span><strong>{{.TokenStatus}}</strong></div>
</div>
{{template "payload" .Input}}
{{template "payload" .Sampling}}
{{template "payload" .Returned}}
{{if .ReturnedMessages}}
<h4>Returned model messages</h4>
{{range .ReturnedMessages}}
{{template "payload" .Payload}}
{{if .HasText}}<div class="rationale"><p class="label">Model's stated rationale</p><pre>{{.Content}}</pre></div>{{end}}
{{end}}
{{end}}
{{with .Tokens}}<p>Server-reported tokens: {{.Input}} input · {{.Output}} output · {{.Total}} total.</p>{{end}}
{{with .Error}}<p>Turn error: {{.}}</p>{{end}}
{{if .Partial}}<p class="empty">Partial turn: inference or capture did not complete.</p>{{end}}
</article>
{{end}}
{{end}}
</section>
{{end}}
{{else}}
<p class="empty">No model turns were captured.</p>
{{end}}
</body>
</html>
{{define "payload"}}
{{with .}}<details class="payload">
<summary>{{.Label}}</summary>
<pre>{{.JSON}}</pre>
</details>{{end}}
{{end}}
`))
