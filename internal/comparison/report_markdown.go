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
	output.WriteString("\n## Interaction evidence\n")

	for _, interaction := range document.Interactions {
		writeMarkdownInteraction(&output, interaction)
	}

	return output.String()
}

func writeMarkdownSummary(output *strings.Builder, document report) {
	output.WriteString("## Summary\n\n")
	fmt.Fprintf(output, "- Total interactions: %d\n", document.Summary.InteractionCount)
	writeMarkdownProblemCount(output, document.Baseline)
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
	fmt.Fprintf(output, "\n> Problem-level outcomes are unavailable: %s\n", document.ProblemOutcomesNote)
}

func writeMarkdownInteraction(
	output *strings.Builder,
	interaction reportInteractionEvidence,
) {
	fmt.Fprintf(
		output,
		"\n### Interaction %d: `%s %s`\n\n",
		interaction.Interaction,
		interaction.Request.Method,
		interaction.Request.URL,
	)
	fmt.Fprintf(output, "- Candidate target: `%s`\n", interaction.TargetURL)
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
