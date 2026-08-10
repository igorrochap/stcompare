// Package scorecard joins comparison and benchmark records into an HTML report.
package scorecard

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"stcompare/benchrecord"
	"stcompare/internal/comparison"
)

type benchmarkView struct {
	Agent        string
	Model        string
	Iterations   int
	TotalTime    string
	AgentFixTime string
	Tokens       *benchrecord.TokenUsage
}

// Render renders a benchmark scorecard containing the complete comparison report.
func Render(document comparison.Report, record benchrecord.Record) (string, error) {
	comparisonHTML, err := comparison.RenderHTML(document)
	if err != nil {
		return "", fmt.Errorf("render comparison: %w", err)
	}

	var section bytes.Buffer
	view := benchmarkView{
		Agent:        record.Agent,
		Model:        record.Model,
		Iterations:   record.Iterations,
		TotalTime:    formatMilliseconds(record.TimeMS.Total),
		AgentFixTime: formatMilliseconds(record.TimeMS.AgentFix),
		Tokens:       record.Tokens,
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
</section>
`))
