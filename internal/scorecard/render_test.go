package scorecard

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestRenderLinksToAvailableAuditAndDoesNotInventLegacyActivity(t *testing.T) {
	document := comparisonFixture(t)
	audited, err := Render(document, benchrecord.Record{Audit: benchrecord.AuditReference{
		Status: benchrecord.AuditStatusComplete,
		Report: "benchmark-audit.html",
	}})
	if err != nil {
		t.Fatalf("render audited scorecard: %v", err)
	}
	if !strings.Contains(audited, `href="benchmark-audit.html"`) ||
		!strings.Contains(audited, "View chronological model-turn audit") {
		t.Fatalf("audited scorecard missing audit link:\n%s", audited)
	}

	legacy, err := Render(document, benchrecord.Record{})
	if err != nil {
		t.Fatalf("render legacy scorecard: %v", err)
	}
	if !strings.Contains(legacy, "Audit evidence: not reported") {
		t.Fatalf("legacy scorecard missing not-reported audit state:\n%s", legacy)
	}
	if strings.Contains(legacy, "model-turn audit\" (complete)") {
		t.Fatal("legacy scorecard invented audit activity")
	}
}

func TestRenderShowsModelToolCallSummaryWhenAuditActivityIsAvailable(t *testing.T) {
	document := comparisonFixture(t)
	activity := &benchrecord.ActivitySummary{
		Status: benchrecord.ActivityStatusComplete,
		ModelToolCalls: benchrecord.ActivityCounts{
			Count: 3, Completed: 2, Failed: 1, DurationMS: 125,
		},
		AdapterOperations: benchrecord.ActivityCounts{Count: 3, DurationMS: 120},
		EditAttempts:      4,
		FileModifications: 3,
	}
	html, err := Render(document, benchrecord.Record{Audit: benchrecord.AuditReference{
		Status:   benchrecord.AuditStatusComplete,
		Report:   "benchmark-audit.html",
		Activity: activity,
	}})
	if err != nil {
		t.Fatalf("render scorecard: %v", err)
	}
	for _, fragment := range []string{
		"Model Tool Call activity",
		"Model Tool Calls</span><strong>3</strong>",
		"Completed</span><strong>2</strong>",
		"Failed</span><strong>1</strong>",
		"Execution time</span><strong>125 ms</strong>",
		"Edit Attempts</span><strong>4</strong>",
		"File Modifications</span><strong>3</strong>",
		"Adapter Operations: 3 (120 ms execution time)",
	} {
		if !strings.Contains(html, fragment) {
			t.Fatalf("scorecard missing activity summary %q:\n%s", fragment, html)
		}
	}
}

func TestBuildLoadsActivityFromReferencedAuditWhenRecordIsLegacy(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "comparison.json")
	recordPath := filepath.Join(directory, "benchmark-record.json")
	activityPath := filepath.Join(directory, "benchmark-audit.json")
	outputPath := filepath.Join(directory, "scorecard.html")

	comparisonContents, err := json.Marshal(comparisonFixture(t))
	if err != nil {
		t.Fatalf("marshal comparison fixture: %v", err)
	}
	if err := os.WriteFile(comparisonPath, comparisonContents, 0o644); err != nil {
		t.Fatalf("write comparison fixture: %v", err)
	}
	if err := os.WriteFile(recordPath, []byte(`{"audit":{"status":"complete","artifact":"benchmark-audit.json"}}`), 0o644); err != nil {
		t.Fatalf("write record fixture: %v", err)
	}
	if err := os.WriteFile(activityPath, []byte(`{
  "schema_version":"1",
  "capture":{"enabled":true,"status":"complete","complete":true},
  "iterations":[],
  "events":[{"type":"model_tool_call","status":"completed","duration_ms":17}]
}`), 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}

	if err := Build(Input{ComparisonPath: comparisonPath, RecordPath: recordPath, OutputPath: outputPath}); err != nil {
		t.Fatalf("build scorecard: %v", err)
	}
	html, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read scorecard: %v", err)
	}
	if !strings.Contains(string(html), "Model Tool Calls</span><strong>1</strong>") {
		t.Fatalf("scorecard omitted referenced audit activity:\n%s", html)
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
