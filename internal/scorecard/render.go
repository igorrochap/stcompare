// Package scorecard joins comparison and benchmark records into an HTML report.
package scorecard

import (
	"bytes"
	_ "embed"
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
	TerminalState       benchrecord.TerminalState
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
	PromptSizeLimit     *benchrecord.PromptSizeLimit
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
		TerminalState:       record.TerminalState,
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
		PromptSizeLimit:     record.PromptSizeLimit,
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

//go:embed scorecard.gohtml
var benchmarkSectionTemplateText string

var benchmarkSectionTemplate = template.Must(template.New("benchmark-run").Parse(benchmarkSectionTemplateText))
