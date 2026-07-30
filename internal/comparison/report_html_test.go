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

func TestRenderHTMLShowsBaselineProblemBreakdownMeter(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:        10,
				Evaluable:    6,
				Fixed:        4,
				StillFailing: 1,
				Inconclusive: 1,
				Unevaluable:  2,
				Uncorrelated: 1,
				Ambiguous:    1,
				FixRate:      baselineProblemFixRate{Meaning: fixRateMeaning},
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"Baseline problem breakdown",
		"aria-label=\"Baseline problem bucket breakdown\"",
		"style=\"width: 40.0000%;\"",
		"style=\"width: 10.0000%;\"",
		"style=\"width: 20.0000%;\"",
		"Fixed</span><strong>4</strong>",
		"Still failing</span><strong>1</strong>",
		"Inconclusive</span><strong>1</strong>",
		"Unevaluable</span><strong>2</strong>",
		"Uncorrelated</span><strong>1</strong>",
		"Ambiguous</span><strong>1</strong>",
		"Total baseline problems</span><strong>10</strong>",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
	if strings.Contains(html, "Problem outcomes") ||
		strings.Contains(html, "problem outcome breakdown") ||
		strings.Contains(html, "outcome meter") {
		t.Fatalf("renderHTML used glossary-confusing outcome label:\n%s", html)
	}
}

func TestRenderHTMLShowsExplicitEmptyAndUnavailableProblemBreakdownMeterStates(t *testing.T) {
	tests := []struct {
		name     string
		document report
		want     string
	}{
		{
			name: "zero baseline problems",
			document: report{
				BaselineProblemsAvailable: true,
				Summary: reportSummary{
					BaselineProblems: baselineProblemSummary{
						Total:   0,
						FixRate: baselineProblemFixRate{Meaning: fixRateMeaning},
					},
				},
			},
			want: "No baseline problems were extracted, so there is no problem breakdown to show.",
		},
		{
			name: "unavailable baseline problems",
			document: report{
				BaselineProblemsAvailable: false,
				BaselineProblemsNote:      baselineProblemsUnavailable,
				Summary: reportSummary{
					BaselineProblems: baselineProblemSummary{
						FixRate: baselineProblemFixRate{Meaning: fixRateMeaning},
					},
				},
			},
			want: "Baseline problem breakdown is unavailable.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := mustRenderHTML(t, tt.document)

			if !strings.Contains(html, tt.want) {
				t.Fatalf("renderHTML missing %q:\n%s", tt.want, html)
			}
			if strings.Contains(html, "style=\"width: NaN") ||
				strings.Contains(html, "style=\"width: +Inf") {
				t.Fatalf("renderHTML rendered broken meter width:\n%s", html)
			}
		})
	}
}

func TestRenderHTMLShowsSeparateTrafficClassifications(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:   1,
				Fixed:   1,
				FixRate: baselineProblemFixRate{Meaning: fixRateMeaning},
			},
			Traffic: trafficSummary{
				Total:            9,
				SuccessUnchanged: 6,
				Changed:          2,
				Regressed:        1,
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"<section class=\"traffic\"",
		"Traffic classifications",
		"Success unchanged</span><strong>6</strong>",
		"Changed</span><strong>2</strong>",
		"Regressed</span><strong>1</strong>",
		"Traffic total</span><strong>9</strong>",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
}

func TestRenderHTMLShowsCaveats(t *testing.T) {
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:        7,
				Inconclusive: 2,
				Unevaluable:  1,
				Uncorrelated: 3,
				Ambiguous:    1,
				UnevaluableByCheckCategory: []unevaluableCheckCategory{
					{CheckCategory: checkCategoryUncategorized, Count: 1},
				},
				FixRate: baselineProblemFixRate{Meaning: fixRateMeaning},
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"<section class=\"caveats\"",
		"Caveats",
		"Inconclusive</span><strong>2</strong>",
		"Uncorrelated</span><strong>3</strong>",
		"Ambiguous</span><strong>1</strong>",
		"Unevaluable</span><strong>1</strong>",
		"Unevaluable by check category",
		"uncategorized</span><strong>1</strong>",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
}

func TestRenderHTMLShowsProblemListsGroupedByOutcome(t *testing.T) {
	interaction := 2
	baselineStatus := 500
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:        5,
				Fixed:        1,
				StillFailing: 1,
				Inconclusive: 1,
				Uncorrelated: 1,
				Ambiguous:    1,
				FixRate:      baselineProblemFixRate{Meaning: fixRateMeaning},
			},
		},
		Problems: []baselineProblem{
			{
				CheckName:     "not_a_server_error",
				CheckCategory: checkCategoryServerError,
				Message:       "returned 200",
				Outcome:       problemOutcomeFixed,
			},
			{
				CheckName:     "status_code_conformance",
				CheckCategory: checkCategoryStatusCodeConformance,
				Message:       "Undocumented HTTP status code",
				Outcome:       problemOutcomeStillFailing,
				Reproduction: problemReproduction{
					Command: `schemathesis run --hypothesis-seed=123 http://api.example/openapi.json`,
				},
				Interaction: &interaction,
			},
			{
				CheckName:     "response_schema_conformance",
				CheckCategory: checkCategoryResponseSchemaConformance,
				Message:       "No matching replay interaction",
				Outcome:       problemOutcomeInconclusive,
				OutcomeReason: problemOutcomeReasonSchemaOperationNotFound,
			},
			{
				CheckName:     "negative_data_rejection",
				CheckCategory: checkCategoryNegativeDataRejection,
				Message:       "No matching replay interaction",
				Outcome:       problemOutcomeNotEvaluated,
				OutcomeReason: problemOutcomeReasonUncorrelatedEvidence,
			},
			{
				CheckName:     "positive_data_acceptance",
				CheckCategory: checkCategoryPositiveDataAcceptance,
				Message:       "Matched multiple replay interactions",
				Outcome:       problemOutcomeNotEvaluated,
				OutcomeReason: problemOutcomeReasonAmbiguousCorrelation,
			},
		},
		Findings: []reportInteractionEvidence{
			{
				Interaction: 1,
				StatusTransition: statusTransition{
					Baseline:  &baselineStatus,
					Candidate: 200,
				},
			},
			{
				Interaction: 2,
				StatusTransition: statusTransition{
					Baseline:  &baselineStatus,
					Candidate: 422,
				},
			},
		},
	}

	html := mustRenderHTML(t, document)

	want := []string{
		"<section class=\"problem-lists\"",
		"Fixed <span>1</span>",
		"Still failing <span>1</span>",
		"<span>Inconclusive 1</span>",
		"Not evaluated <span>2</span>",
		"not_a_server_error",
		"server_error",
		"returned 200",
		"status_code_conformance",
		"Undocumented HTTP status code",
		"<details class=\"problem-entry problem-entry-expandable\">",
		"schemathesis run --hypothesis-seed=123 http://api.example/openapi.json",
		"Status transition</span><strong>500 -&gt; 422</strong>",
		"response_schema_conformance",
		"response_schema_conformance - Inconclusive",
		"No matching replay interaction",
		"negative_data_rejection",
		"positive_data_acceptance",
		"Matched multiple replay interactions",
		"<details class=\"problem-group problem-group-subordinate\">",
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing %q:\n%s", fragment, html)
		}
	}
	if strings.Contains(html, "500 -&gt; 200") {
		t.Fatalf("renderHTML matched status transition without using interaction index:\n%s", html)
	}
	lists := newHTMLProblemListsView(document)
	if lists.StillFailing.Count != document.Summary.BaselineProblems.StillFailing {
		t.Fatalf(
			"still-failing list count = %d, want summary still-failing count %d",
			lists.StillFailing.Count,
			document.Summary.BaselineProblems.StillFailing,
		)
	}
	if lists.StillFailing.InconclusiveCount != document.Summary.BaselineProblems.Inconclusive {
		t.Fatalf(
			"inconclusive list count = %d, want summary inconclusive count %d",
			lists.StillFailing.InconclusiveCount,
			document.Summary.BaselineProblems.Inconclusive,
		)
	}
	got := lists.Fixed.Count + lists.StillFailing.Count +
		lists.StillFailing.InconclusiveCount + lists.NotEvaluated.Count
	if got != document.Summary.BaselineProblems.Total {
		t.Fatalf("problem list count sum = %d, want summary total %d", got, document.Summary.BaselineProblems.Total)
	}
}

func TestRenderHTMLShowsProblemListEmptyStatesAndHidesListsWhenProblemsUnavailable(t *testing.T) {
	available := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				FixRate: baselineProblemFixRate{Meaning: fixRateMeaning},
			},
		},
	}

	html := mustRenderHTML(t, available)

	if got := strings.Count(html, "<p class=\"empty\">None</p>"); got != 3 {
		t.Fatalf("renderHTML None count = %d, want 3:\n%s", got, html)
	}

	unavailable := report{
		BaselineProblemsAvailable: false,
		BaselineProblemsNote:      baselineProblemsUnavailable,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				FixRate: baselineProblemFixRate{Meaning: fixRateMeaning},
			},
		},
	}

	html = mustRenderHTML(t, unavailable)

	if strings.Contains(html, "<section class=\"problem-lists\"") ||
		strings.Contains(html, "<p class=\"empty\">None</p>") {
		t.Fatalf("renderHTML showed problem lists for unavailable baseline problems:\n%s", html)
	}
}

func TestRenderHTMLShowsFallbackReproductionRequestAndEscapesProblemText(t *testing.T) {
	interaction := 1
	baselineStatus := 500
	document := report{
		BaselineProblemsAvailable: true,
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Total:        1,
				StillFailing: 1,
				FixRate:      baselineProblemFixRate{Meaning: fixRateMeaning},
			},
		},
		Problems: []baselineProblem{
			{
				CheckName:     `check<script>`,
				CheckCategory: checkCategoryServerError,
				Message:       `message</div>`,
				Outcome:       problemOutcomeStillFailing,
				Reproduction: problemReproduction{
					Method: "POST",
					URL:    `http://api.example/widgets?q=<script>`,
				},
				Interaction: &interaction,
			},
		},
		Findings: []reportInteractionEvidence{
			{
				Interaction: 1,
				StatusTransition: statusTransition{
					Baseline:  &baselineStatus,
					Candidate: 500,
				},
			},
		},
	}

	html := mustRenderHTML(t, document)

	rejected := []string{
		`check<script>`,
		`message</div>`,
		`http://api.example/widgets?q=<script>`,
	}
	for _, fragment := range rejected {
		if strings.Contains(html, fragment) {
			t.Fatalf("renderHTML included unsafe problem text %q:\n%s", fragment, html)
		}
	}
	want := []string{
		`check&lt;script&gt;`,
		`message&lt;/div&gt;`,
		`POST http://api.example/widgets?q=&lt;script&gt;`,
	}
	for _, fragment := range want {
		if !strings.Contains(html, fragment) {
			t.Fatalf("renderHTML missing escaped problem text %q:\n%s", fragment, html)
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
