package scorecard

import (
	"encoding/json"
	"strings"
	"testing"

	"stcompare/benchrecord"
	"stcompare/internal/comparison"
)

func TestRenderAddsBenchmarkRunToComparisonHTML(t *testing.T) {
	document := comparisonFixture(t)
	record := benchrecord.Record{
		Agent:      "codex",
		Model:      "gpt-5.6",
		Iterations: 3,
		TimeMS: benchrecord.TimeBreakdown{
			Total:    272000,
			AgentFix: 65250,
		},
		Tokens: &benchrecord.TokenUsage{
			Input:  1200,
			Output: 345,
			Total:  1545,
		},
	}

	comparisonHTML, err := comparison.RenderHTML(document)
	if err != nil {
		t.Fatalf("render comparison HTML: %v", err)
	}
	scorecardHTML, err := Render(document, record)
	if err != nil {
		t.Fatalf("render scorecard HTML: %v", err)
	}

	withoutBenchmark := removeBenchmarkSection(t, scorecardHTML)
	if withoutBenchmark != comparisonHTML {
		t.Fatal("scorecard HTML without Benchmark Run section differs from comparison HTML")
	}
	problemBreakdown := strings.Index(scorecardHTML, `<section class="problem-breakdown">`)
	benchmarkRun := strings.Index(scorecardHTML, `<section class="benchmark-run">`)
	traffic := strings.Index(scorecardHTML, `<section class="traffic">`)
	if problemBreakdown < 0 || benchmarkRun < problemBreakdown || traffic < benchmarkRun {
		t.Fatal("Benchmark Run section is not between problem breakdown and traffic classifications")
	}
	for _, fragment := range []string{
		"Benchmark Run",
		"Agent</span><strong>codex</strong>",
		"Model</span><strong>gpt-5.6</strong>",
		"Total time</span><strong>4m 32s</strong>",
		"Agent-fix time</span><strong>1m 5s</strong>",
		"Iterations</span><strong>3</strong>",
		"Input tokens</span><strong>1200</strong>",
		"Output tokens</span><strong>345</strong>",
		"Total tokens</span><strong>1545</strong>",
	} {
		if !strings.Contains(scorecardHTML, fragment) {
			t.Fatalf("scorecard HTML missing %q:\n%s", fragment, scorecardHTML)
		}
	}
}

func TestRenderStatesWhenTokenUsageWasNotReported(t *testing.T) {
	html, err := Render(comparisonFixture(t), benchrecord.Record{})
	if err != nil {
		t.Fatalf("render scorecard HTML: %v", err)
	}

	for _, fragment := range []string{"Token usage", "not reported"} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("scorecard HTML missing %q:\n%s", fragment, html)
		}
	}
	for _, fragment := range []string{"Input tokens", "Output tokens", "Total tokens"} {
		if strings.Contains(html, fragment) {
			t.Fatalf("scorecard HTML includes %q for unreported token usage:\n%s", fragment, html)
		}
	}
}

func comparisonFixture(t *testing.T) comparison.Report {
	t.Helper()

	const fixture = `{
		"schema_version": "11",
		"baseline": {"campaign": "baseline"},
		"candidate": {"campaign": "candidate", "base_url": "http://candidate.test"},
		"baseline_problems_available": true,
		"summary": {
			"baseline_problems": {
				"total": 2,
				"evaluable": 2,
				"fixed": 1,
				"still_failing": 1,
				"fix_rate": {
					"available": true,
					"fixed": 1,
					"denominator": 2,
					"percentage": 50,
					"meaning": "fixture meaning"
				}
			},
			"traffic": {"total": 3, "success_unchanged": 1, "changed": 1, "regressed": 1}
		},
		"problems": []
	}`

	var document comparison.Report
	if err := json.Unmarshal([]byte(fixture), &document); err != nil {
		t.Fatalf("unmarshal comparison fixture: %v", err)
	}

	return document
}

func removeBenchmarkSection(t *testing.T, html string) string {
	t.Helper()

	const opening = `<section class="benchmark-run">`
	start := strings.Index(html, opening)
	if start < 0 {
		t.Fatalf("scorecard HTML missing benchmark section:\n%s", html)
	}
	endOffset := strings.Index(html[start:], "</section>\n")
	if endOffset < 0 {
		t.Fatalf("scorecard HTML benchmark section is not closed:\n%s", html)
	}
	end := start + endOffset + len("</section>\n")

	return html[:start] + html[end:]
}
