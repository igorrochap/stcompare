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
	ProblemBreakdown htmlProblemBreakdownView
	Traffic          htmlTrafficView
	Caveats          htmlCaveatsView
	ProblemLists     htmlProblemListsView
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

type htmlProblemBreakdownView struct {
	HasTotal  bool
	EmptyText string
	Buckets   []htmlProblemBucketView
}

type htmlProblemBucketView struct {
	Label string
	Count int
	Class string
	Width template.CSS
}

type htmlTrafficView struct {
	Total            int
	SuccessUnchanged int
	Changed          int
	Regressed        int
}

type htmlCaveatsView struct {
	Inconclusive                 int
	Unevaluable                  int
	Uncorrelated                 int
	Ambiguous                    int
	UnevaluableByCheckCategories []unevaluableCheckCategory
}

type htmlProblemListsView struct {
	Show         bool
	Fixed        htmlProblemGroupView
	StillFailing htmlProblemGroupView
	NotEvaluated htmlProblemGroupView
}

type htmlProblemGroupView struct {
	Count             int
	InconclusiveCount int
	Problems          []htmlProblemEntryView
}

type htmlProblemEntryView struct {
	CheckName        string
	CheckCategory    checkCategory
	Message          string
	OutcomeLabel     string
	Reproduction     string
	StatusTransition string
	HasDetails       bool
}

// RenderHTML renders a comparison report as a self-contained HTML document.
func RenderHTML(document Report) (string, error) {
	var output bytes.Buffer
	if err := comparisonHTMLTemplate.Execute(&output, newHTMLReportView(document)); err != nil {
		return "", fmt.Errorf("render comparison HTML report: %w", err)
	}

	return output.String(), nil
}

func writeHTMLReport(path string, document report) error {
	html, err := RenderHTML(document)
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
		ProblemBreakdown: newHTMLProblemBreakdownView(
			document.BaselineProblemsAvailable,
			document.Summary.BaselineProblems,
		),
		Traffic: newHTMLTrafficView(document.Summary.Traffic),
		Caveats: newHTMLCaveatsView(
			document.Summary.BaselineProblems,
		),
		ProblemLists: newHTMLProblemListsView(document),
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

func newHTMLProblemBreakdownView(
	baselineProblemsAvailable bool,
	summary baselineProblemSummary,
) htmlProblemBreakdownView {
	if !baselineProblemsAvailable {
		return htmlProblemBreakdownView{
			EmptyText: "Baseline problem breakdown is unavailable.",
		}
	}
	if summary.Total == 0 {
		return htmlProblemBreakdownView{
			HasTotal:  false,
			EmptyText: "No baseline problems were extracted, so there is no problem breakdown to show.",
		}
	}

	return htmlProblemBreakdownView{
		HasTotal: true,
		Buckets: []htmlProblemBucketView{
			newHTMLProblemBucketView("Fixed", summary.Fixed, summary.Total, "bucket-fixed"),
			newHTMLProblemBucketView("Still failing", summary.StillFailing, summary.Total, "bucket-still-failing"),
			newHTMLProblemBucketView("Inconclusive", summary.Inconclusive, summary.Total, "bucket-inconclusive"),
			newHTMLProblemBucketView("Unevaluable", summary.Unevaluable, summary.Total, "bucket-unevaluable"),
			newHTMLProblemBucketView("Uncorrelated", summary.Uncorrelated, summary.Total, "bucket-uncorrelated"),
			newHTMLProblemBucketView("Ambiguous", summary.Ambiguous, summary.Total, "bucket-ambiguous"),
		},
	}
}

func newHTMLProblemBucketView(
	label string,
	count int,
	total int,
	class string,
) htmlProblemBucketView {
	return htmlProblemBucketView{
		Label: label,
		Count: count,
		Class: class,
		Width: template.CSS(fmt.Sprintf("%.4f%%", float64(count)*100/float64(total))),
	}
}

func newHTMLTrafficView(summary trafficSummary) htmlTrafficView {
	return htmlTrafficView{
		Total:            summary.Total,
		SuccessUnchanged: summary.SuccessUnchanged,
		Changed:          summary.Changed,
		Regressed:        summary.Regressed,
	}
}

func newHTMLCaveatsView(summary baselineProblemSummary) htmlCaveatsView {
	return htmlCaveatsView{
		Inconclusive:                 summary.Inconclusive,
		Unevaluable:                  summary.Unevaluable,
		Uncorrelated:                 summary.Uncorrelated,
		Ambiguous:                    summary.Ambiguous,
		UnevaluableByCheckCategories: summary.UnevaluableByCheckCategory,
	}
}

func newHTMLProblemListsView(document report) htmlProblemListsView {
	if !document.BaselineProblemsAvailable {
		return htmlProblemListsView{}
	}

	view := htmlProblemListsView{
		Show: true,
	}

	transitionsByInteraction := make(map[int]statusTransition, len(document.Findings))
	for _, finding := range document.Findings {
		transitionsByInteraction[finding.Interaction] = finding.StatusTransition
	}

	for _, problem := range document.Problems {
		entry := newHTMLProblemEntryView(problem, transitionsByInteraction)
		switch problem.Outcome {
		case problemOutcomeFixed:
			view.Fixed.Problems = append(view.Fixed.Problems, entry)
		case problemOutcomeStillFailing:
			view.StillFailing.Problems = append(view.StillFailing.Problems, entry)
			view.StillFailing.Count++
		case problemOutcomeInconclusive:
			entry.OutcomeLabel = "Inconclusive"
			view.StillFailing.Problems = append(view.StillFailing.Problems, entry)
			view.StillFailing.InconclusiveCount++
		case problemOutcomeNotEvaluated:
			view.NotEvaluated.Problems = append(view.NotEvaluated.Problems, entry)
		}
	}

	view.Fixed.Count = len(view.Fixed.Problems)
	view.NotEvaluated.Count = len(view.NotEvaluated.Problems)

	return view
}

func newHTMLProblemEntryView(
	problem baselineProblem,
	transitionsByInteraction map[int]statusTransition,
) htmlProblemEntryView {
	entry := htmlProblemEntryView{
		CheckName:     problem.CheckName,
		CheckCategory: problem.CheckCategory,
		Message:       problem.Message,
		Reproduction:  htmlProblemReproduction(problem.Reproduction),
	}

	if problem.Interaction == nil {
		entry.HasDetails = entry.Reproduction != "" || entry.StatusTransition != ""
		return entry
	}
	transition, ok := transitionsByInteraction[*problem.Interaction]
	if !ok {
		entry.HasDetails = entry.Reproduction != "" || entry.StatusTransition != ""
		return entry
	}
	entry.StatusTransition = htmlStatusTransition(transition)
	entry.HasDetails = entry.Reproduction != "" || entry.StatusTransition != ""

	return entry
}

func htmlProblemReproduction(reproduction problemReproduction) string {
	if reproduction.Command != "" {
		return reproduction.Command
	}
	if reproduction.Method == "" && reproduction.URL == "" {
		return ""
	}
	if reproduction.Method == "" {
		return reproduction.URL
	}
	if reproduction.URL == "" {
		return reproduction.Method
	}

	return reproduction.Method + " " + reproduction.URL
}

func htmlStatusTransition(transition statusTransition) string {
	if transition.Baseline == nil {
		return fmt.Sprintf("unknown -> %d", transition.Candidate)
	}

	return fmt.Sprintf("%d -> %d", *transition.Baseline, transition.Candidate)
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
	--fixed: #0f7b53;
	--still-failing: #b42318;
	--inconclusive: #b86e00;
	--unevaluable: #626a76;
	--uncorrelated: #3267a8;
	--ambiguous: #7a4fb8;
	--segment-text: #ffffff;
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
		--fixed: #42c990;
		--still-failing: #ff8f86;
		--inconclusive: #f0b45b;
		--unevaluable: #aab4c0;
		--uncorrelated: #7bb3f0;
		--ambiguous: #bc9cf4;
		--segment-text: #111418;
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
section {
	padding: 18px;
	margin-bottom: 18px;
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
.section-lede {
	margin: 0 0 14px;
	color: var(--muted);
}
.meter {
	display: flex;
	overflow: hidden;
	min-height: 34px;
	border: 1px solid var(--border);
	border-radius: 8px;
	background: var(--bg);
}
.meter-segment {
	min-width: 0;
	color: var(--segment-text);
	font-size: 12px;
	font-weight: 700;
	line-height: 34px;
	text-align: center;
	white-space: nowrap;
}
.meter-segment[data-count="0"] {
	color: transparent;
}
.bucket-fixed { background: var(--fixed); }
.bucket-still-failing { background: var(--still-failing); }
.bucket-inconclusive { background: var(--inconclusive); }
.bucket-unevaluable { background: var(--unevaluable); }
.bucket-uncorrelated { background: var(--uncorrelated); }
.bucket-ambiguous { background: var(--ambiguous); }
.counts {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
	gap: 10px;
	margin-top: 14px;
}
.count {
	border: 1px solid var(--border);
	border-radius: 8px;
	padding: 12px;
	background: var(--bg);
	min-width: 0;
}
.count strong {
	display: block;
	font-size: 24px;
	line-height: 1.2;
}
.traffic .counts {
	grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
}
.category-counts {
	margin-top: 14px;
}
.category-counts h3 {
	margin: 0 0 10px;
	font-size: 15px;
}
.problem-groups {
	display: grid;
	gap: 14px;
}
.problem-group {
	border: 1px solid var(--border);
	border-radius: 8px;
	background: var(--bg);
	padding: 14px;
}
.problem-group h3, .problem-group > summary {
	margin: 0 0 10px;
	font-size: 16px;
	font-weight: 700;
}
.problem-group > summary {
	cursor: pointer;
}
.problem-group h3 span, .problem-group > summary span {
	color: var(--muted);
	font-size: 13px;
	font-weight: 600;
}
.problem-group-subordinate {
	background: transparent;
}
.problem-entries {
	display: grid;
	gap: 8px;
}
.problem-entry {
	border: 1px solid var(--border);
	border-radius: 8px;
	background: var(--panel);
	padding: 10px 12px;
}
.problem-entry-summary {
	display: grid;
	gap: 2px;
}
.problem-entry summary.problem-entry-summary {
	cursor: pointer;
}
.problem-entry-title {
	font-weight: 700;
	overflow-wrap: anywhere;
}
.problem-entry-meta, .problem-entry-detail span {
	color: var(--muted);
	font-size: 13px;
}
.problem-entry-message {
	overflow-wrap: anywhere;
}
.problem-entry-details {
	display: grid;
	gap: 8px;
	margin-top: 10px;
}
.problem-entry-detail strong {
	display: block;
	overflow-wrap: anywhere;
	font-weight: 600;
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
<section class="problem-breakdown">
<h2>Baseline problem breakdown</h2>
<p class="section-lede">Baseline Schemathesis problems partitioned by the report summary.</p>
{{if .ProblemBreakdown.HasTotal}}
<div class="meter" role="img" aria-label="Baseline problem bucket breakdown">
{{range .ProblemBreakdown.Buckets}}<div class="meter-segment {{.Class}}" style="width: {{.Width}};" data-count="{{.Count}}">{{.Label}} {{.Count}}</div>{{end}}
</div>
{{else}}
<p class="empty">{{.ProblemBreakdown.EmptyText}}</p>
{{end}}
<div class="counts">
{{range .ProblemBreakdown.Buckets}}
<div class="count"><span>{{.Label}}</span><strong>{{.Count}}</strong></div>
{{end}}
<div class="count"><span>Total baseline problems</span><strong>{{.Document.Summary.BaselineProblems.Total}}</strong></div>
</div>
</section>
<section class="traffic">
<h2>Traffic classifications</h2>
<p class="section-lede">Candidate replay traffic, separate from problem outcomes.</p>
<div class="counts">
<div class="count"><span>Success unchanged</span><strong>{{.Traffic.SuccessUnchanged}}</strong></div>
<div class="count"><span>Changed</span><strong>{{.Traffic.Changed}}</strong></div>
<div class="count"><span>Regressed</span><strong>{{.Traffic.Regressed}}</strong></div>
<div class="count"><span>Traffic total</span><strong>{{.Traffic.Total}}</strong></div>
</div>
</section>
{{if .ProblemLists.Show}}
<section class="problem-lists">
<h2>Baseline problems</h2>
<p class="section-lede">Compact lists of extracted baseline Schemathesis problems by comparison outcome.</p>
<div class="problem-groups">
<div class="problem-group">
<h3>Fixed <span>{{.ProblemLists.Fixed.Count}}</span></h3>
{{if .ProblemLists.Fixed.Problems}}
<div class="problem-entries">
{{range .ProblemLists.Fixed.Problems}}
<div class="problem-entry">
<div class="problem-entry-summary">
<span class="problem-entry-title">{{.CheckName}}</span>
<span class="problem-entry-meta">{{.CheckCategory}}</span>
<span class="problem-entry-message">{{.Message}}</span>
</div>
</div>
{{end}}
</div>
{{else}}
<p class="empty">None</p>
{{end}}
</div>
<div class="problem-group">
<h3>Still failing <span>{{.ProblemLists.StillFailing.Count}}</span>{{if .ProblemLists.StillFailing.InconclusiveCount}} <span>Inconclusive {{.ProblemLists.StillFailing.InconclusiveCount}}</span>{{end}}</h3>
{{if .ProblemLists.StillFailing.Problems}}
<div class="problem-entries">
{{range .ProblemLists.StillFailing.Problems}}
{{if .HasDetails}}
<details class="problem-entry problem-entry-expandable">
<summary class="problem-entry-summary">
<span class="problem-entry-title">{{.CheckName}}</span>
<span class="problem-entry-meta">{{.CheckCategory}}{{if .OutcomeLabel}} - {{.OutcomeLabel}}{{end}}</span>
<span class="problem-entry-message">{{.Message}}</span>
</summary>
<div class="problem-entry-details">
{{if .Reproduction}}<div class="problem-entry-detail"><span>Reproduction</span><strong>{{.Reproduction}}</strong></div>{{end}}
{{if .StatusTransition}}<div class="problem-entry-detail"><span>Status transition</span><strong>{{.StatusTransition}}</strong></div>{{end}}
</div>
</details>
{{else}}
<div class="problem-entry">
<div class="problem-entry-summary">
<span class="problem-entry-title">{{.CheckName}}</span>
<span class="problem-entry-meta">{{.CheckCategory}}{{if .OutcomeLabel}} - {{.OutcomeLabel}}{{end}}</span>
<span class="problem-entry-message">{{.Message}}</span>
</div>
</div>
{{end}}
{{end}}
</div>
{{else}}
<p class="empty">None</p>
{{end}}
</div>
<details class="problem-group problem-group-subordinate">
<summary>Not evaluated <span>{{.ProblemLists.NotEvaluated.Count}}</span></summary>
{{if .ProblemLists.NotEvaluated.Problems}}
<div class="problem-entries">
{{range .ProblemLists.NotEvaluated.Problems}}
<div class="problem-entry">
<div class="problem-entry-summary">
<span class="problem-entry-title">{{.CheckName}}</span>
<span class="problem-entry-meta">{{.CheckCategory}}</span>
<span class="problem-entry-message">{{.Message}}</span>
</div>
</div>
{{end}}
</div>
{{else}}
<p class="empty">None</p>
{{end}}
</details>
</div>
</section>
{{end}}
<section class="caveats">
<h2>Caveats</h2>
<p class="section-lede">Counts that limit how much of the baseline problem set could contribute to the fix rate.</p>
<div class="counts">
<div class="count"><span>Inconclusive</span><strong>{{.Caveats.Inconclusive}}</strong></div>
<div class="count"><span>Uncorrelated</span><strong>{{.Caveats.Uncorrelated}}</strong></div>
<div class="count"><span>Ambiguous</span><strong>{{.Caveats.Ambiguous}}</strong></div>
<div class="count"><span>Unevaluable</span><strong>{{.Caveats.Unevaluable}}</strong></div>
</div>
<div class="category-counts">
<h3>Unevaluable by check category</h3>
{{if .Caveats.UnevaluableByCheckCategories}}
<div class="counts">
{{range .Caveats.UnevaluableByCheckCategories}}
<div class="count"><span>{{.CheckCategory}}</span><strong>{{.Count}}</strong></div>
{{end}}
</div>
{{else}}
<p class="empty">No unevaluable check categories.</p>
{{end}}
</div>
</section>
</main>
</body>
</html>
`))
