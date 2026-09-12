// Package audit reads, finalizes, and renders the local-model turn audit.
package audit

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"stcompare/benchrecord"
)

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
