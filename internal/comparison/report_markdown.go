package comparison

import (
	"fmt"
	"os"
	"strings"
)

func renderMarkdown(document report) string {
	var output strings.Builder
	output.WriteString("# Campaign comparison\n\n")
	output.WriteString("## Summary\n\n")
	fmt.Fprintf(&output, "- Total interactions: %d\n", document.Summary.InteractionCount)
	if document.Baseline.ProblemCount == nil {
		output.WriteString("- Baseline problems: unknown\n")
	} else if document.Baseline.ProblemCountSource == nil {
		fmt.Fprintf(&output, "- Baseline problems: %d\n", *document.Baseline.ProblemCount)
	} else {
		fmt.Fprintf(
			&output,
			"- Baseline problems: %d (source: `%s`)\n",
			*document.Baseline.ProblemCount,
			*document.Baseline.ProblemCountSource,
		)
	}
	fmt.Fprintf(
		&output,
		"- Candidate latency: minimum %d ms, maximum %d ms, average %d ms\n",
		document.Summary.LatencyMS.Minimum,
		document.Summary.LatencyMS.Maximum,
		document.Summary.LatencyMS.Average,
	)
	if len(document.Summary.StatusTransitions) == 0 {
		output.WriteString("- Exact status transitions: none\n")
	} else {
		output.WriteString("- Exact status transitions:\n")
		for _, transition := range document.Summary.StatusTransitions {
			fmt.Fprintf(
				&output,
				"  - `%d -> %d`: %d\n",
				transition.Baseline,
				transition.Candidate,
				transition.Count,
			)
		}
	}
	fmt.Fprintf(&output, "- Baseline campaign: `%s`\n", document.Baseline.Campaign)
	fmt.Fprintf(&output, "- Candidate campaign: `%s`\n", document.Candidate.Campaign)
	fmt.Fprintf(&output, "- Candidate base URL: `%s`\n", document.Candidate.BaseURL)
	output.WriteString("\n## Findings\n")

	for _, finding := range document.Findings {
		fmt.Fprintf(
			&output,
			"\n### Interaction %d: `%s %s`\n\n",
			finding.Interaction,
			finding.Request.Method,
			finding.Request.URL,
		)
		fmt.Fprintf(&output, "- Candidate target: `%s`\n", finding.TargetURL)
		fmt.Fprintf(&output, "- Latency: %d ms\n", finding.LatencyMS)
		if finding.StatusTransition.Baseline == nil {
			fmt.Fprintf(
				&output,
				"- Status transition: `unknown -> %d`\n",
				finding.StatusTransition.Candidate,
			)
		} else {
			fmt.Fprintf(
				&output,
				"- Status transition: `%d -> %d`\n",
				*finding.StatusTransition.Baseline,
				finding.StatusTransition.Candidate,
			)
		}

		output.WriteString("\n#### Request headers\n\n")
		writeMarkdownHeaders(&output, finding.Request.Headers)
		output.WriteString("\n#### Request body\n\n")
		writeMarkdownBody(&output, finding.Request.Body)

		if finding.BaselineResponse == nil {
			output.WriteString("\n#### Baseline response: unknown\n")
		} else {
			fmt.Fprintf(
				&output,
				"\n#### Baseline response: `%d`\n\nHeaders:\n\n",
				finding.BaselineResponse.Status,
			)
			writeMarkdownHeaders(&output, finding.BaselineResponse.Headers)
			output.WriteString("\nBody:\n\n")
			writeMarkdownBody(&output, finding.BaselineResponse.Body)
		}

		fmt.Fprintf(
			&output,
			"\n#### Candidate response: `%d`\n\nHeaders:\n\n",
			finding.CandidateResponse.Status,
		)
		writeMarkdownHeaders(&output, finding.CandidateResponse.Headers)
		output.WriteString("\nBody:\n\n")
		writeMarkdownBody(&output, finding.CandidateResponse.Body)
	}

	return output.String()
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
