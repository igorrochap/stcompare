package comparison

import (
	"fmt"
	"os"
	"strings"
)

func renderMarkdown(document report) string {
	var output strings.Builder
	output.WriteString("# Campaign comparison\n\n")
	writeMarkdownSummary(&output, document)
	writeMarkdownComparisonPolicy(&output, document.ComparisonPolicy)
	if len(document.Problems) != 0 {
		writeMarkdownProblems(&output, document.Problems)
	}
	output.WriteString("\n## Findings\n")

	for _, interaction := range document.Findings {
		writeMarkdownInteraction(&output, interaction)
	}

	return output.String()
}

func writeMarkdownSummary(output *strings.Builder, document report) {
	output.WriteString("## Summary\n\n")
	fmt.Fprintf(output, "- Total interactions: %d\n", document.Summary.InteractionCount)
	writeMarkdownProblemCount(output, document.Baseline)
	writeMarkdownExtractedProblemCount(output, document.Baseline)
	writeMarkdownBaselineProblemSummary(output, document.Summary.BaselineProblems)
	writeMarkdownTrafficSummary(output, document.Summary.Traffic)
	fmt.Fprintf(
		output,
		"- Candidate latency: minimum %d ms, maximum %d ms, average %d ms\n",
		document.Summary.LatencyMS.Minimum,
		document.Summary.LatencyMS.Maximum,
		document.Summary.LatencyMS.Average,
	)
	writeMarkdownStatusTransitions(output, document.Summary.StatusTransitions)
	fmt.Fprintf(output, "- Baseline campaign: `%s`\n", document.Baseline.Campaign)
	fmt.Fprintf(output, "- Candidate campaign: `%s`\n", document.Candidate.Campaign)
	fmt.Fprintf(output, "- Candidate base URL: `%s`\n", document.Candidate.BaseURL)
	if !document.BaselineProblemsAvailable {
		fmt.Fprintf(
			output,
			"\n> Baseline Schemathesis problems are unavailable: %s\n",
			document.BaselineProblemsNote,
		)
	}
}

func writeMarkdownComparisonPolicy(
	output *strings.Builder,
	policy PreconditionPolicy,
) {
	output.WriteString("\n## Comparison policy\n\n")
	if len(policy.MissingResourceStatuses) == 0 {
		output.WriteString("- Missing resource statuses: none\n")
	} else {
		output.WriteString("- Missing resource statuses: ")
		for index, status := range policy.MissingResourceStatuses {
			if index != 0 {
				output.WriteString(", ")
			}
			fmt.Fprintf(output, "`%d`", status)
		}
		output.WriteString("\n")
	}

	if len(policy.Heuristics) == 0 {
		output.WriteString("- Precondition heuristics: none\n")
	} else {
		output.WriteString("- Precondition heuristics:\n")
		for _, heuristic := range policy.Heuristics {
			fmt.Fprintf(
				output,
				"  - `%s`: method `%s`, path pattern `%s`\n",
				heuristic.Name,
				heuristic.Method,
				heuristic.PathPattern,
			)
		}
	}
	writeMarkdownNormalizationPolicy(output, policy.Normalization)
}

func writeMarkdownNormalizationPolicy(
	output *strings.Builder,
	normalization ResponseNormalizationConfig,
) {
	if !normalization.Defaults &&
		len(normalization.BodyFields) == 0 &&
		len(normalization.Headers) == 0 {
		output.WriteString("- Normalization: none\n")
		return
	}

	if normalization.Defaults {
		output.WriteString("- Normalization defaults: enabled\n")
	} else {
		output.WriteString("- Normalization defaults: disabled\n")
	}
	if len(normalization.BodyFields) != 0 {
		output.WriteString("- Normalized body fields:\n")
		for _, rule := range normalization.BodyFields {
			fmt.Fprintf(
				output,
				"  - `%s`: field `%s`\n",
				rule.Name,
				rule.FieldName,
			)
		}
	}
	if len(normalization.Headers) != 0 {
		output.WriteString("- Normalized headers:\n")
		for _, rule := range normalization.Headers {
			fmt.Fprintf(
				output,
				"  - `%s`: header `%s`\n",
				rule.Name,
				rule.HeaderName,
			)
		}
	}
}

func writeMarkdownProblems(output *strings.Builder, problems []baselineProblem) {
	output.WriteString("\n## Baseline problems\n")
	for index, problem := range problems {
		writeMarkdownProblem(output, index+1, problem)
	}
}

func writeMarkdownProblem(
	output *strings.Builder,
	number int,
	problem baselineProblem,
) {
	fmt.Fprintf(output, "\n### Problem %d: `%s`\n\n", number, problem.CheckName)
	fmt.Fprintf(output, "- Check category: `%s`\n", problem.CheckCategory)
	fmt.Fprintf(output, "- Message: %s\n", problem.Message)
	fmt.Fprintf(output, "- Evidence source: `%s`\n", problem.EvidenceSource)
	fmt.Fprintf(output, "- Case ID: `%s`\n", problem.CaseID)
	if problem.Outcome != "" {
		fmt.Fprintf(output, "- Outcome: `%s`\n", problem.Outcome)
	}
	if problem.OutcomeReason != "" {
		fmt.Fprintf(output, "- Outcome reason: `%s`\n", problem.OutcomeReason)
	}
	if len(problem.ExerciseEvidence) != 0 {
		output.WriteString("- Exercise evidence: ")
		for index, evidence := range problem.ExerciseEvidence {
			if index != 0 {
				output.WriteString(", ")
			}
			fmt.Fprintf(output, "`%s`", evidence)
		}
		output.WriteString("\n")
	}
	if problem.MatchedPreconditionHeuristic != "" {
		fmt.Fprintf(
			output,
			"- Matched precondition heuristic: `%s`\n",
			problem.MatchedPreconditionHeuristic,
		)
	}
	switch problem.CorrelationStatus {
	case correlationStatusCorrelated:
		if problem.Interaction == nil {
			output.WriteString("- Correlation: uncorrelated\n")
			break
		}
		fmt.Fprintf(output, "- Correlation: interaction %d\n", *problem.Interaction)
	case correlationStatusAmbiguous:
		output.WriteString("- Correlation: ambiguous\n")
	default:
		output.WriteString("- Correlation: uncorrelated\n")
	}

	writeMarkdownProblemReproduction(output, problem.Reproduction)
}

func writeMarkdownProblemReproduction(
	output *strings.Builder,
	reproduction problemReproduction,
) {
	if reproduction.Command != "" {
		output.WriteString("\n#### Reproduction command\n\n```shell\n")
		output.WriteString(reproduction.Command)
		output.WriteString("\n```\n")
		return
	}
	if reproduction.Method == "" &&
		reproduction.URL == "" &&
		len(reproduction.Headers) == 0 &&
		reproduction.Body == "" {
		return
	}

	output.WriteString("\n#### Reproduction request\n\n")
	fmt.Fprintf(output, "- Method: `%s`\n", reproduction.Method)
	fmt.Fprintf(output, "- URL: `%s`\n", reproduction.URL)
	output.WriteString("\nHeaders:\n\n")
	writeMarkdownHeaders(output, reproduction.Headers)
	output.WriteString("\nBody:\n\n")
	writeMarkdownBody(output, reproduction.Body)
}

func writeMarkdownInteraction(
	output *strings.Builder,
	interaction reportInteractionEvidence,
) {
	fmt.Fprintf(
		output,
		"\n### Finding %d: `%s %s`\n\n",
		interaction.Interaction,
		interaction.Request.Method,
		interaction.Request.URL,
	)
	fmt.Fprintf(output, "- Candidate target: `%s`\n", interaction.TargetURL)
	if interaction.Classification != "" {
		fmt.Fprintf(output, "- Classification: `%s`\n", interaction.Classification)
	}
	fmt.Fprintf(output, "- Latency: %d ms\n", interaction.LatencyMS)
	writeMarkdownStatusTransition(output, interaction.StatusTransition)

	writeMarkdownRequest(output, interaction.Request)

	if interaction.BaselineResponse == nil {
		output.WriteString("\n#### Baseline response: unknown\n")
	} else {
		writeMarkdownResponse(output, "Baseline", *interaction.BaselineResponse)
	}

	writeMarkdownResponse(output, "Candidate", interaction.CandidateResponse)
}

func writeMarkdownRequest(output *strings.Builder, request reportRequest) {
	output.WriteString("\n#### Request headers\n\n")
	writeMarkdownHeaders(output, request.Headers)
	output.WriteString("\n#### Request body\n\n")
	writeMarkdownBody(output, request.Body)
}

func writeMarkdownStatusTransition(
	output *strings.Builder,
	transition statusTransition,
) {
	if transition.Baseline == nil {
		fmt.Fprintf(
			output,
			"- Status transition: `unknown -> %d`\n",
			transition.Candidate,
		)
		return
	}

	fmt.Fprintf(
		output,
		"- Status transition: `%d -> %d`\n",
		*transition.Baseline,
		transition.Candidate,
	)
}

func writeMarkdownProblemCount(output *strings.Builder, baseline reportCampaign) {
	if baseline.ProblemCount == nil {
		output.WriteString("- Baseline problems: unknown\n")
	} else if baseline.ProblemCountSource == nil {
		fmt.Fprintf(output, "- Baseline problems: %d\n", *baseline.ProblemCount)
	} else {
		fmt.Fprintf(
			output,
			"- Baseline problems: %d (source: `%s`)\n",
			*baseline.ProblemCount,
			*baseline.ProblemCountSource,
		)
	}
}

func writeMarkdownExtractedProblemCount(output *strings.Builder, baseline reportCampaign) {
	if baseline.ExtractedProblemCount == nil {
		return
	}
	if baseline.ExtractedProblemCountSource == nil {
		fmt.Fprintf(output, "- Extracted baseline problems: %d\n", *baseline.ExtractedProblemCount)
		return
	}

	fmt.Fprintf(
		output,
		"- Extracted baseline problems: %d (source: `%s`)\n",
		*baseline.ExtractedProblemCount,
		*baseline.ExtractedProblemCountSource,
	)
}

func writeMarkdownBaselineProblemSummary(
	output *strings.Builder,
	summary baselineProblemSummary,
) {
	const summaryFormat = "- Baseline problem outcomes: total %d, evaluable %d, fixed %d, " +
		"still failing %d, inconclusive %d, unevaluable %d, " +
		"uncorrelated %d, ambiguous %d\n"

	fmt.Fprintf(
		output,
		summaryFormat,
		summary.Total,
		summary.Evaluable,
		summary.Fixed,
		summary.StillFailing,
		summary.Inconclusive,
		summary.Unevaluable,
		summary.Uncorrelated,
		summary.Ambiguous,
	)
	writeMarkdownBaselineProblemFixRate(output, summary.FixRate)
}

func writeMarkdownBaselineProblemFixRate(
	output *strings.Builder,
	rate baselineProblemFixRate,
) {
	const unavailableRateFormat = "- Fix rate: unavailable (%d evaluable baseline problems). %s\n"
	const availableRateFormat = "- Fix rate: %d/%d evaluable baseline problems fixed (%.1f%%). %s\n"

	if !rate.Available {
		fmt.Fprintf(
			output,
			unavailableRateFormat,
			rate.Denominator,
			rate.Meaning,
		)
		return
	}

	fmt.Fprintf(
		output,
		availableRateFormat,
		rate.Fixed,
		rate.Denominator,
		*rate.Percentage,
		rate.Meaning,
	)
}

func writeMarkdownTrafficSummary(output *strings.Builder, summary trafficSummary) {
	fmt.Fprintf(
		output,
		"- Traffic classifications: total %d, success unchanged %d, changed %d, regressed %d\n",
		summary.Total,
		summary.SuccessUnchanged,
		summary.Changed,
		summary.Regressed,
	)
}

func writeMarkdownStatusTransitions(
	output *strings.Builder,
	transitions []statusTransition,
) {
	if len(transitions) == 0 {
		output.WriteString("- Exact status transitions: none\n")
		return
	}

	output.WriteString("- Exact status transitions:\n")
	for _, transition := range transitions {
		fmt.Fprintf(
			output,
			"  - `%d -> %d`: %d\n",
			*transition.Baseline,
			transition.Candidate,
			transition.Count,
		)
	}
}

func writeMarkdownResponse(
	output *strings.Builder,
	label string,
	response reportResponse,
) {
	fmt.Fprintf(
		output,
		"\n#### %s response: `%d`\n\nHeaders:\n\n",
		label,
		response.Status,
	)
	writeMarkdownHeaders(output, response.Headers)
	output.WriteString("\nBody:\n\n")
	writeMarkdownBody(output, response.Body)
}

func writeMarkdownHeaders(output *strings.Builder, headers []harHeader) {
	if len(headers) == 0 {
		output.WriteString("_None._\n")
		return
	}

	output.WriteString("```text\n")
	for _, header := range headers {
		fmt.Fprintf(output, "%s: %s\n", header.Name, header.Value)
	}
	output.WriteString("```\n")
}

func writeMarkdownBody(output *strings.Builder, body string) {
	if body == "" {
		output.WriteString("_Empty._\n")
		return
	}

	output.WriteString("```text\n")
	output.WriteString(body)
	output.WriteString("\n```\n")
}

func writeMarkdownReport(path string, document report) error {
	if err := os.WriteFile(path, []byte(renderMarkdown(document)), 0o644); err != nil {
		return fmt.Errorf("write comparison Markdown report: %w", err)
	}

	return nil
}
