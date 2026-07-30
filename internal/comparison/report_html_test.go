package comparison

import (
	"html/template"
	"strings"
	"testing"
)

func TestRenderHTMLShowsHeadlineMetricsAndCampaignIdentity(t *testing.T) {
	percentage := 66.7
	document := report{
		BaselineProblemsAvailable: true,
		Baseline: reportCampaign{
			Campaign: "baseline",
		},
		Candidate: reportCandidate{
			Campaign: "gpt5.6",
			BaseURL:  "http://candidate.example.test",
		},
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:        5,
				Evaluable:    3,
				Unevaluable:  1,
				Uncorrelated: 1,
				Fixed:        2,
				StillFailing: 1,
				Inconclusive: 0,
				FixRate: baselineProblemFixRate{
					Available:        true,
					Fixed:            2,
					Denominator:      3,
					DenominatorBasis: fixRateDenominatorBasis,
					Percentage:       &percentage,
					Meaning:          fixRateMeaning,
				},
			},
			Traffic: trafficSummary{
				Total:            4,
				SuccessUnchanged: 2,
				Changed:          1,
				Regressed:        0,
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"<title>Campaign comparison scorecard</title>",
		"Baseline</span><strong>baseline</strong>",
		"Candidate</span><strong>gpt5.6</strong>",
		"Candidate base URL</span><strong>http://candidate.example.test</strong>",
		"Fix rate",
		"66.7%",
		"2 / 3 evaluable baseline problems fixed",
		"0 regressions",
		"class=\"metric metric-calm\"",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
	rejected := []string{"<script", " src=", " href=", "@import", "https://"}
	for _, fragment := range rejected {
		if strings.Contains(html, fragment) {
			t.Fatalf("renderHTML included external asset or script hook %q:\n%s", fragment, html)
		}
	}
}

func TestRenderHTMLShowsUnavailableFixRateForZeroEvaluableProblems(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				FixRate: baselineProblemFixRate{
					Available:   false,
					Denominator: 0,
					Meaning:     fixRateMeaning,
					Note:        fixRateZeroDenominatorNote,
				},
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"Fix rate",
		"unavailable",
		fixRateZeroDenominatorNote,
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
	if strings.Contains(html, "0%") {
		t.Fatalf("renderHTML rendered unavailable fix rate as zero:\n%s", html)
	}
}

func TestRenderHTMLStatesUnavailableBaselineProblemsProminently(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: false,
		BaselineProblemsNote:      baselineProblemsUnavailable,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				FixRate: baselineProblemFixRate{
					Available:   false,
					Denominator: 0,
					Meaning:     fixRateMeaning,
				},
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"Baseline Schemathesis problems unavailable",
		baselineProblemsUnavailable,
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
	if strings.Contains(html, "0%") {
		t.Fatalf("renderHTML rendered unavailable baseline problems as zero:\n%s", html)
	}
}

func TestRenderHTMLEmphasizesNonzeroRegressionCount(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			Traffic: trafficSummary{Regressed: 2},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"class=\"metric metric-alarm\"",
		"2 regressions",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
}

func TestRenderHTMLEscapesDynamicText(t *testing.T) {
	percentage := 100.0
	document := report{
		BaselineProblemsAvailable: true,
		Baseline: reportCampaign{
			Campaign: `baseline<script>alert("x")</script>`,
		},
		Candidate: reportCandidate{
			Campaign: `candidate</div>`,
			BaseURL:  `http://candidate.example.test/?q=<script>`,
		},
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				FixRate: baselineProblemFixRate{
					Available:   true,
					Fixed:       1,
					Denominator: 1,
					Percentage:  &percentage,
				},
			},
		},
	}

	html := mustRenderHTML(t, document)

	for _, unsafe := range []string{
		`<script>alert("x")</script>`,
		`candidate</div>`,
	} {
		if strings.Contains(html, unsafe) {
			t.Fatalf("renderHTML included unsafe text %q:\n%s", unsafe, html)
		}
	}
	wantEscaped := []string{
		`baseline&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`,
		`candidate&lt;/div&gt;`,
	}
	for _, fragment := range wantEscaped {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing escaped text %q:\n%s", fragment, html)
		}
	}
}

func TestWriteHTMLReportReturnsTemplateExecutionError(t *testing.T) {
	originalTemplate := comparisonHTMLTemplate
	t.Cleanup(func() {
		comparisonHTMLTemplate = originalTemplate
	})
	comparisonHTMLTemplate = template.Must(template.New("bad-html").Parse("{{.Missing.Field}}"))

	err := writeHTMLReport(t.TempDir()+"/comparison.html", report{})

	if err == nil {
		t.Fatal("writeHTMLReport error = nil, want template execution error")
	}
	if !strings.Contains(err.Error(), "render comparison HTML report:") {
		t.Fatalf("writeHTMLReport error = %q, want render context", err.Error())
	}
}

func mustRenderHTML(t *testing.T, document report) string {
	t.Helper()

	html, err := renderHTML(document)
	if err != nil {
		t.Fatalf("renderHTML error = %v", err)
	}

	return html
}
