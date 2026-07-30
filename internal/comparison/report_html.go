package comparison

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
)

type htmlReportView struct {
	Document         report
	FixRate          htmlFixRateView
	RegressionMetric htmlRegressionMetricView
}

type htmlFixRateView struct {
	Available bool
	Value     string
	Fraction  string
	Note      string
}

type htmlRegressionMetricView struct {
	Count int
	Class string
	Label string
}

func renderHTML(document report) (string, error) {
	var output bytes.Buffer
	if err := comparisonHTMLTemplate.Execute(&output, newHTMLReportView(document)); err != nil {
		return "", fmt.Errorf("render comparison HTML report: %w", err)
	}

	return output.String(), nil
}

func writeHTMLReport(path string, document report) error {
	html, err := renderHTML(document)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		return fmt.Errorf("write comparison HTML report: %w", err)
	}

	return nil
}

func newHTMLReportView(document report) htmlReportView {
	return htmlReportView{
		Document:         document,
		FixRate:          newHTMLFixRateView(document.Summary.BaselineProblems.FixRate),
		RegressionMetric: newHTMLRegressionMetricView(document.Summary.Traffic.Regressed),
	}
}

func newHTMLFixRateView(rate baselineProblemFixRate) htmlFixRateView {
	if !rate.Available || rate.Percentage == nil {
		note := rate.Note
		if note == "" {
			note = rate.Meaning
		}
		return htmlFixRateView{
			Available: false,
			Value:     "unavailable",
			Fraction:  fmt.Sprintf("%d evaluable baseline problems", rate.Denominator),
			Note:      note,
		}
	}

	return htmlFixRateView{
		Available: true,
		Value:     fmt.Sprintf("%.1f%%", *rate.Percentage),
		Fraction:  fmt.Sprintf("%d / %d evaluable baseline problems fixed", rate.Fixed, rate.Denominator),
		Note:      rate.Meaning,
	}
}

func newHTMLRegressionMetricView(count int) htmlRegressionMetricView {
	class := "metric metric-calm"
	if count != 0 {
		class = "metric metric-alarm"
	}

	return htmlRegressionMetricView{
		Count: count,
		Class: class,
		Label: pluralize(count, "regression", "regressions"),
	}
}

func pluralize(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}

	return plural
}

var comparisonHTMLTemplate = template.Must(template.New("comparison-html").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Campaign comparison scorecard</title>
<style>
:root {
	color-scheme: light dark;
	--bg: #f7f8fb;
	--panel: #ffffff;
	--text: #17202a;
	--muted: #5c6672;
	--border: #d9dee7;
	--accent: #126d5b;
	--accent-bg: #e3f4ef;
	--alarm: #b42318;
	--alarm-bg: #fee4e2;
	--calm-bg: #eef4ff;
}
@media (prefers-color-scheme: dark) {
	:root {
		--bg: #121416;
		--panel: #1d2126;
		--text: #edf1f5;
		--muted: #aab4c0;
		--border: #39414c;
		--accent: #64d4b6;
		--accent-bg: #123c34;
		--alarm: #ff9b93;
		--alarm-bg: #4b1f1d;
		--calm-bg: #1d2b45;
	}
}
* { box-sizing: border-box; }
body {
	margin: 0;
	background: var(--bg);
	color: var(--text);
	font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
	line-height: 1.5;
}
main {
	max-width: 1100px;
	margin: 0 auto;
	padding: 32px 20px 48px;
}
header {
	display: grid;
	gap: 16px;
	margin-bottom: 24px;
}
h1 {
	margin: 0;
	font-size: 28px;
	font-weight: 700;
}
h2 {
	margin: 0 0 12px;
	font-size: 18px;
}
.identity {
	display: grid;
	grid-template-columns: repeat(3, minmax(0, 1fr));
	gap: 10px;
}
.identity div, section, .metric {
	border: 1px solid var(--border);
	background: var(--panel);
	border-radius: 8px;
}
.identity div {
	padding: 12px;
	min-width: 0;
}
.identity span, .metric span, .count span {
	display: block;
	color: var(--muted);
	font-size: 13px;
}
.identity strong {
	display: block;
	overflow-wrap: anywhere;
}
.notice {
	border: 1px solid var(--alarm);
	background: var(--alarm-bg);
	border-radius: 8px;
	padding: 14px 16px;
	margin-bottom: 18px;
}
.notice strong {
	display: block;
	margin-bottom: 4px;
}
.metrics {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: 14px;
	margin-bottom: 18px;
}
.metric {
	padding: 18px;
}
.metric strong {
	display: block;
	font-size: 36px;
	line-height: 1.1;
	margin: 8px 0;
}
.metric p {
	margin: 0;
	color: var(--muted);
}
.metric-primary {
	background: var(--accent-bg);
	border-color: var(--accent);
}
.metric-alarm {
	background: var(--alarm-bg);
	border-color: var(--alarm);
}
.metric-calm {
	background: var(--calm-bg);
}
.empty {
	color: var(--muted);
}
@media (max-width: 720px) {
	.identity, .metrics {
		grid-template-columns: 1fr;
	}
	main {
		padding-inline: 14px;
	}
}
</style>
</head>
<body>
<main>
<header>
<h1>Campaign comparison scorecard</h1>
<div class="identity">
<div><span>Baseline</span><strong>{{.Document.Baseline.Campaign}}</strong></div>
<div><span>Candidate</span><strong>{{.Document.Candidate.Campaign}}</strong></div>
<div><span>Candidate base URL</span><strong>{{.Document.Candidate.BaseURL}}</strong></div>
</div>
</header>
{{if not .Document.BaselineProblemsAvailable}}
<div class="notice">
<strong>Baseline Schemathesis problems unavailable</strong>
<span>{{.Document.BaselineProblemsNote}}</span>
</div>
{{end}}
<div class="metrics">
<div class="metric metric-primary">
<span>Fix rate</span>
<strong>{{.FixRate.Value}}</strong>
<p>{{.FixRate.Fraction}}</p>
{{if .FixRate.Note}}<p>{{.FixRate.Note}}</p>{{end}}
</div>
<div class="{{.RegressionMetric.Class}}">
<span>Regressions</span>
<strong>{{.RegressionMetric.Count}} {{.RegressionMetric.Label}}</strong>
<p>Candidate traffic classified as regressed.</p>
</div>
</div>
{{if not .Document.BaselineProblemsAvailable}}
<p class="empty">Problem outcomes were not measured.</p>
{{end}}
</main>
</body>
</html>
`))
