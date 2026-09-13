package comparison

import (
	"bytes"
	_ "embed"
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

//go:embed comparison.gohtml
var comparisonHTMLTemplateText string

var comparisonHTMLTemplate = template.Must(template.New("comparison-html").Parse(comparisonHTMLTemplateText))
