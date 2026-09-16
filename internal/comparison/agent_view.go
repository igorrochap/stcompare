package comparison

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"stcompare/agentreport"
)

const (
	agentEvidenceBodyCap    = 512
	agentEvidenceDetailsCap = 5
	agentEvidenceRefsCap    = 3
)

type agentStatusKey struct {
	value int
	set   bool
}

type problemGroupKey struct {
	kind          agentreport.ActionKind
	operation     string
	checkCategory agentreport.CheckCategory
	message       string
	baseline      agentStatusKey
	candidate     agentStatusKey
}

type problemGroup struct {
	key      problemGroupKey
	status   agentreport.Status
	count    int
	refs     []int
	seenRefs map[int]struct{}
	sample   agentreport.Sample
}

func newAgentView(document report) agentreport.View {
	view := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Converged:     document.Converged,
		Candidate:     document.Candidate.Campaign,
		Baseline:      document.Baseline.Campaign,
		Counts: agentreport.Counts{
			Fixed:        document.Summary.BaselineProblems.Fixed,
			StillFailing: document.Summary.BaselineProblems.StillFailing,
			Regressed:    document.Summary.Traffic.Regressed,
		},
		Unverified: agentreport.Unverified{
			Inconclusive: document.Summary.BaselineProblems.Inconclusive,
			Uncorrelated: document.Summary.BaselineProblems.Uncorrelated,
			Ambiguous:    document.Summary.BaselineProblems.Ambiguous,
			Unevaluable:  document.Summary.BaselineProblems.Unevaluable,
		},
	}

	groups := make(map[problemGroupKey]*problemGroup)
	for _, problem := range document.Problems {
		if problem.Outcome != problemOutcomeStillFailing || problem.Interaction == nil {
			continue
		}
		interaction, ok := interactionByReference(document.allInteractions, *problem.Interaction)
		if !ok {
			continue
		}
		status := interactionStatus(interaction)
		key := problemGroupKey{
			kind:          agentreport.ActionKindStillFailing,
			operation:     operationTemplate(document.schemaValidation, interaction),
			checkCategory: agentreport.CheckCategory(problem.CheckCategory),
			message:       oneLine(problem.Message),
			baseline:      statusKey(status.Baseline),
			candidate:     statusKey(status.Candidate),
		}
		addProblemToGroup(groups, key, status, interaction, problemDetails(problem))
	}
	for _, interaction := range document.Findings {
		if interaction.Classification != interactionClassificationRegressed {
			continue
		}
		status := interactionStatus(interaction)
		key := problemGroupKey{
			kind:          agentreport.ActionKindRegressed,
			operation:     operationTemplate(document.schemaValidation, interaction),
			checkCategory: agentreport.CheckCategoryTrafficRegression,
			message:       regressionMessage(interaction),
			baseline:      statusKey(status.Baseline),
			candidate:     statusKey(status.Candidate),
		}
		addProblemToGroup(groups, key, status, interaction, nil)
	}

	view.Actionable = actionableGroups(groups)
	return view
}

func addProblemToGroup(
	groups map[problemGroupKey]*problemGroup,
	key problemGroupKey,
	status agentreport.Status,
	interaction reportInteractionEvidence,
	details []string,
) {
	group, ok := groups[key]
	if !ok {
		group = &problemGroup{
			key:      key,
			status:   status,
			refs:     make([]int, 0, agentEvidenceRefsCap),
			seenRefs: make(map[int]struct{}),
		}
		groups[key] = group
	}

	ref := interaction.Interaction
	if _, ok := group.seenRefs[ref]; ok {
		return
	}
	group.seenRefs[ref] = struct{}{}
	group.count++
	group.refs = append(group.refs, ref)
	sort.Ints(group.refs)
	if len(group.refs) > agentEvidenceRefsCap {
		group.refs = group.refs[:agentEvidenceRefsCap]
	}
	if group.count == 1 || ref < group.sample.Ref {
		group.sample = newAgentSample(interaction, details)
	}
}

func actionableGroups(groups map[problemGroupKey]*problemGroup) []agentreport.Actionable {
	actionable := make([]agentreport.Actionable, 0, len(groups))
	for _, group := range groups {
		actionable = append(actionable, agentreport.Actionable{
			ID:            stableProblemGroupID(group.key),
			Kind:          group.key.kind,
			CheckCategory: group.key.checkCategory,
			Operation:     group.key.operation,
			Status:        group.status,
			Message:       group.key.message,
			Count:         group.count,
			Refs:          append([]int(nil), group.refs...),
			Sample:        group.sample,
		})
	}

	sort.SliceStable(actionable, func(i, j int) bool {
		left, right := actionable[i], actionable[j]
		if left.Kind != right.Kind {
			return left.Kind == agentreport.ActionKindRegressed
		}
		if left.Operation != right.Operation {
			return left.Operation < right.Operation
		}
		if left.CheckCategory != right.CheckCategory {
			return left.CheckCategory < right.CheckCategory
		}
		if statusOrder := compareStatuses(left.Status.Baseline, right.Status.Baseline); statusOrder != 0 {
			return statusOrder < 0
		}
		if statusOrder := compareStatuses(left.Status.Candidate, right.Status.Candidate); statusOrder != 0 {
			return statusOrder < 0
		}
		if left.Message != right.Message {
			return left.Message < right.Message
		}
		return left.ID < right.ID
	})

	return actionable
}

func newAgentSample(
	interaction reportInteractionEvidence,
	details []string,
) agentreport.Sample {
	return agentreport.Sample{
		Ref:          interaction.Interaction,
		Operation:    interactionOperation(interaction),
		RequestBody:  truncateAgentBody(interaction.Request.Body),
		ResponseBody: truncateAgentBody(interaction.CandidateResponse.Body),
		Details:      boundedDetails(details),
	}
}

func problemDetails(problem baselineProblem) []string {
	if problem.CheckCategory != checkCategoryResponseSchemaConformance {
		return []string{}
	}

	return problem.SchemaValidationErrors
}

func boundedDetails(details []string) []string {
	bounded := make([]string, 0, min(len(details), agentEvidenceDetailsCap))
	for _, detail := range details {
		if len(bounded) == agentEvidenceDetailsCap {
			break
		}
		bounded = append(bounded, detail)
	}

	return bounded
}

func truncateAgentBody(body string) string {
	if len(body) <= agentEvidenceBodyCap {
		return body
	}

	representation := jsonSafeAgentBody(body)
	prefixEnd := agentEvidenceBodyCap
	for prefixEnd > 0 && !utf8.RuneStart(representation[prefixEnd]) {
		prefixEnd--
	}

	return representation[:prefixEnd] + fmt.Sprintf("…[truncated %d bytes]", len(representation)-prefixEnd)
}

func jsonSafeAgentBody(body string) string {
	if utf8.ValidString(body) {
		return body
	}

	var representation strings.Builder
	representation.Grow(len(body))
	for offset := 0; offset < len(body); {
		value, size := utf8.DecodeRuneInString(body[offset:])
		if value == utf8.RuneError && size == 1 {
			representation.WriteRune(utf8.RuneError)
		} else {
			representation.WriteString(body[offset : offset+size])
		}
		offset += size
	}

	return representation.String()
}

func operationTemplate(contract *OpenAPIContract, interaction reportInteractionEvidence) string {
	concrete := interactionOperation(interaction)
	if contract == nil || contract.document == nil || contract.limitation != "" {
		return concrete
	}

	route, reason := contract.resolveOperation(interaction.Request.Method, interaction.Request.URL)
	if reason != "" || route == nil {
		return concrete
	}

	return strings.ToUpper(interaction.Request.Method) + " " + route.Path
}

func interactionByReference(findings []reportInteractionEvidence, ref int) (reportInteractionEvidence, bool) {
	for _, finding := range findings {
		if finding.Interaction == ref {
			return finding, true
		}
	}

	return reportInteractionEvidence{}, false
}

func interactionOperation(interaction reportInteractionEvidence) string {
	parsed, err := url.Parse(interaction.Request.URL)
	path := interaction.Request.URL
	if err == nil {
		path = parsed.Path
		if path == "" {
			path = "/"
		}
	}
	return strings.ToUpper(interaction.Request.Method) + " " + path
}

func interactionStatus(interaction reportInteractionEvidence) agentreport.Status {
	return agentreport.Status{
		Baseline:  interaction.StatusTransition.Baseline,
		Candidate: intPointer(interaction.StatusTransition.Candidate),
	}
}

func statusKey(status *int) agentStatusKey {
	if status == nil {
		return agentStatusKey{}
	}

	return agentStatusKey{value: *status, set: true}
}

func compareStatuses(left, right *int) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}

	return 0
}

func intPointer(value int) *int { return &value }

func regressionMessage(interaction reportInteractionEvidence) string {
	return oneLine(fmt.Sprintf(
		"candidate returned status %d; baseline returned %s",
		interaction.CandidateResponse.Status,
		formatStatus(interaction.StatusTransition.Baseline),
	))
}

func stableProblemGroupID(key problemGroupKey) string {
	var encoded strings.Builder
	writeGroupKeyField(&encoded, string(key.kind))
	writeGroupKeyField(&encoded, key.operation)
	writeGroupKeyField(&encoded, string(key.checkCategory))
	writeGroupKeyField(&encoded, key.message)
	writeGroupKeyField(&encoded, stableStatusValue(key.baseline))
	writeGroupKeyField(&encoded, stableStatusValue(key.candidate))

	return fmt.Sprintf("%x", sha256.Sum256([]byte(encoded.String())))
}

func writeGroupKeyField(encoded *strings.Builder, value string) {
	encoded.WriteString(strconv.Itoa(len(value)))
	encoded.WriteString(":")
	encoded.WriteString(value)
}

func stableStatusValue(status agentStatusKey) string {
	if !status.set {
		return "null"
	}

	return strconv.Itoa(status.value)
}

func formatStatus(status *int) string {
	if status == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *status)
}

func oneLine(message string) string {
	return strings.Join(strings.Fields(message), " ")
}
