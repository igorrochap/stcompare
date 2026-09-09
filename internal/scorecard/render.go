// Package scorecard joins comparison and benchmark records into an HTML report.
package scorecard

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"stcompare/benchrecord"
	"stcompare/internal/audit"
	"stcompare/internal/comparison"
)

type benchmarkView struct {
	Agent               string
	Model               string
	Iterations          int
	TotalTime           string
	AgentFixTime        string
	Tokens              *benchrecord.TokenUsage
	Efficiency          benchrecord.EfficiencySummary
	EfficiencyAvailable bool
	Audit               benchrecord.AuditReference
	AuditAvailable      bool
	Activity            *benchrecord.ActivitySummary
	ActivityAvailable   bool
	FinalSource         *audit.FinalSource
}

// Render renders a benchmark scorecard containing the complete comparison report.
func Render(document comparison.Report, record benchrecord.Record) (string, error) {
	return render(document, record, nil)
}

// RenderWithAudit renders a scorecard with source and comparison evidence.
func RenderWithAudit(
	document comparison.Report,
	record benchrecord.Record,
	auditDocument *audit.Artifact,
) (string, error) {
	return render(document, record, auditDocument)
}

func render(
	document comparison.Report,
	record benchrecord.Record,
	auditDocument *audit.Artifact,
) (string, error) {
	comparisonHTML, err := comparison.RenderHTML(document)
	if err != nil {
		return "", fmt.Errorf("render comparison: %w", err)
	}

	var section bytes.Buffer
	view := benchmarkView{
		Agent:               record.Agent,
		Model:               record.Model,
		Iterations:          record.Iterations,
		TotalTime:           formatMilliseconds(record.TimeMS.Total),
		AgentFixTime:        formatMilliseconds(record.TimeMS.AgentFix),
		Tokens:              record.Tokens,
		Efficiency:          record.Efficiency,
		EfficiencyAvailable: efficiencyAvailable(record.Efficiency),
		Audit:               record.Audit,
		AuditAvailable:      auditAvailable(record.Audit),
		Activity:            record.Audit.Activity,
		ActivityAvailable:   activityAvailable(record.Audit),
	}
	if auditDocument != nil && efficiencyAvailable(auditDocument.Efficiency) {
		view.Efficiency = auditDocument.Efficiency
		view.EfficiencyAvailable = true
	}
	if auditDocument != nil && auditDocument.FinalSource.Status != "" {
		view.FinalSource = &auditDocument.FinalSource
	}
	if err := benchmarkSectionTemplate.Execute(&section, view); err != nil {
		return "", fmt.Errorf("render benchmark run: %w", err)
	}

	const trafficSection = `<section class="traffic">`
	if !strings.Contains(comparisonHTML, trafficSection) {
		return "", fmt.Errorf("render scorecard: comparison HTML has no traffic section")
	}

	return strings.Replace(comparisonHTML, trafficSection, section.String()+trafficSection, 1), nil
}

func auditAvailable(reference benchrecord.AuditReference) bool {
	if reference.Report == "" {
		return false
	}
	return reference.Status == benchrecord.AuditStatusComplete || reference.Status == benchrecord.AuditStatusPartial
}

func activityAvailable(reference benchrecord.AuditReference) bool {
	if reference.Activity == nil {
		return false
	}
	if reference.Status != benchrecord.AuditStatusComplete && reference.Status != benchrecord.AuditStatusPartial {
		return false
	}
	activity := reference.Activity
	return activity.Status == benchrecord.ActivityStatusComplete || activity.Status == benchrecord.ActivityStatusPartial
}

func efficiencyAvailable(summary benchrecord.EfficiencySummary) bool {
	return summary.Status == benchrecord.EfficiencyStatusComplete || summary.Status == benchrecord.EfficiencyStatusPartial
}

func formatMilliseconds(milliseconds int64) string {
	duration := (time.Duration(milliseconds) * time.Millisecond).Round(time.Second)
	if duration == 0 {
		return "0s"
	}

	prefix := ""
	if duration < 0 {
		prefix = "-"
		duration = -duration
	}

	parts := make([]string, 0, 3)
	if hours := duration / time.Hour; hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		duration %= time.Hour
	}
	if minutes := duration / time.Minute; minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
		duration %= time.Minute
	}
	if duration > 0 {
		parts = append(parts, duration.String())
	}

	return prefix + strings.Join(parts, " ")
}

var benchmarkSectionTemplate = template.Must(template.New("benchmark-run").Parse(`<section class="benchmark-run">
<h2>Benchmark Run</h2>
<p class="section-lede">Cost of producing the candidate fix.</p>
<div class="identity">
<div><span>Agent</span><strong>{{.Agent}}</strong></div>
<div><span>Model</span><strong>{{.Model}}</strong></div>
</div>
<div class="counts">
<div class="count"><span>Total time</span><strong>{{.TotalTime}}</strong></div>
<div class="count"><span>Agent-fix time</span><strong>{{.AgentFixTime}}</strong></div>
<div class="count"><span>Iterations</span><strong>{{.Iterations}}</strong></div>
</div>
<div class="category-counts">
<h3>Token usage</h3>
{{with .Tokens}}
<div class="counts">
<div class="count"><span>Input tokens</span><strong>{{.Input}}</strong></div>
<div class="count"><span>Output tokens</span><strong>{{.Output}}</strong></div>
<div class="count"><span>Total tokens</span><strong>{{.Total}}</strong></div>
</div>
{{else}}
<p class="empty">not reported</p>
{{end}}
</div>
{{if .EfficiencyAvailable}}
<div class="category-counts">
<h3>Efficiency summary <small>({{.Efficiency.Status}} evidence)</small></h3>
<div class="counts">
<div class="count"><span>Model turns</span><strong>{{.Efficiency.Turns}}</strong></div>
<div class="count"><span>Inference time</span><strong>{{.Efficiency.InferenceMS}} ms</strong><small>{{.Efficiency.MeasuredInferenceTurns}} measured · {{.Efficiency.UnknownInferenceTurns}} unknown</small></div>
<div class="count"><span>Audit-recording overhead</span><strong>{{.Efficiency.RecordingOverheadMS}} ms</strong><small>excluded from inference time</small></div>
<div class="count"><span>Token evidence</span><strong>{{.Efficiency.TokenStatus}}</strong><small>{{.Efficiency.KnownTokenTurns}} known · {{.Efficiency.UnknownTokenTurns}} unknown</small></div>
</div>
{{with .Efficiency.Tokens}}<p>Known token subtotal: {{.Input}} input · {{.Output}} output · {{.Total}} total{{if eq $.Efficiency.TokenStatus "partial"}} (partial){{end}}.</p>{{else}}<p class="empty">Known token subtotal: unknown</p>{{end}}
<p class="empty">Inference time is the server-request boundary captured by the audit. Existing total and agent-fix wall-clock fields include capture overhead.</p>
</div>
{{end}}
<div class="category-counts">
<h3>Model Tool Call activity{{with .Activity}} <small>({{.Status}} evidence)</small>{{end}}</h3>
{{if .ActivityAvailable}}
<div class="counts">
<div class="count"><span>Model Tool Calls</span><strong>{{.Activity.ModelToolCalls.Count}}</strong></div>
<div class="count"><span>Completed</span><strong>{{.Activity.ModelToolCalls.Completed}}</strong></div>
<div class="count"><span>Failed</span><strong>{{.Activity.ModelToolCalls.Failed}}</strong></div>
<div class="count"><span>Incomplete</span><strong>{{.Activity.ModelToolCalls.Incomplete}}</strong></div>
<div class="count"><span>Execution time</span><strong>{{.Activity.ModelToolCalls.DurationMS}} ms</strong></div>
</div>
<div class="counts">
<div class="count"><span>Edit Attempts</span><strong>{{.Activity.EditAttempts}}</strong></div>
<div class="count"><span>File Modifications</span><strong>{{.Activity.FileModifications}}</strong></div>
</div>
<p>Adapter Operations: {{.Activity.AdapterOperations.Count}} ({{.Activity.AdapterOperations.DurationMS}} ms execution time)</p>
{{else}}
<p class="empty">not reported</p>
{{end}}
</div>
{{with .FinalSource}}
<div class="category-counts">
<h3>Final source evidence</h3>
<div class="counts">
<div class="count"><span>Snapshot status</span><strong>{{.Status}}</strong></div>
<div class="count"><span>Files Changed at the End</span><strong>{{.FilesChangedAtEnd}}</strong></div>
</div>
{{if eq .Status "unavailable"}}<p class="empty">Final source: unavailable; a complete net diff was not established.</p>{{else if eq .Status "partial"}}<p class="empty">Final source is partial; the count and diff are incomplete evidence.</p>{{end}}
</div>
{{end}}
{{if .AuditAvailable}}
<p><a href="{{.Audit.Report}}">View chronological model-turn audit</a> ({{.Audit.Status}})</p>
{{else}}
<p class="empty">Audit evidence: not reported</p>
{{end}}
</section>
`))
