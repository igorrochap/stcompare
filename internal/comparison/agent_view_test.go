package comparison

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"stcompare/agentreport"
)

func TestNewAgentViewProjectsActionableFindings(t *testing.T) {
	stillFailingRef := 2
	document := report{
		SchemaVersion: "11",
		Converged:     false,
		Baseline:      reportCampaign{Campaign: "baseline"},
		Candidate:     reportCandidate{Campaign: "candidate"},
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{
				Fixed:        3,
				StillFailing: 1,
				Inconclusive: 4,
				Uncorrelated: 2,
				Ambiguous:    1,
				Unevaluable:  1,
			},
			Traffic: trafficSummary{Regressed: 1},
		},
		Problems: []baselineProblem{
			{
				CheckCategory: checkCategoryResponseSchemaConformance,
				Message:       "schema mismatch\nwith detail",
				CaseID:        "case-still-failing",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &stillFailingRef,
			},
			{
				Outcome: problemOutcomeInconclusive,
			},
		},
		allInteractions: []reportInteractionEvidence{
			{
				Interaction:       1,
				Request:           reportRequest{Method: "POST", URL: "https://example.test/z"},
				Classification:    interactionClassificationRegressed,
				StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
				CandidateResponse: reportResponse{Status: 500},
			},
			{
				Interaction:      2,
				Request:          reportRequest{Method: "get", URL: "https://example.test/a"},
				StatusTransition: statusTransition{Baseline: intPointer(200), Candidate: 200},
			},
		},
		Findings: []reportInteractionEvidence{
			{
				Interaction:       1,
				Request:           reportRequest{Method: "POST", URL: "https://example.test/z"},
				Classification:    interactionClassificationRegressed,
				StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
				CandidateResponse: reportResponse{Status: 500},
			},
		},
	}

	got := newAgentView(document)
	if agentreport.SchemaVersion != "2" {
		t.Fatalf("agent schema constant = %q, want 2", agentreport.SchemaVersion)
	}
	if got.SchemaVersion != agentreport.SchemaVersion {
		t.Fatalf("agent schema version = %q, want 2", got.SchemaVersion)
	}
	if !reflect.DeepEqual(got.Counts, (agentreport.Counts{Fixed: 3, StillFailing: 1, Regressed: 1})) {
		t.Fatalf("agent counts = %#v", got.Counts)
	}
	if !reflect.DeepEqual(got.Unverified, (agentreport.Unverified{Inconclusive: 4, Uncorrelated: 2, Ambiguous: 1, Unevaluable: 1})) {
		t.Fatalf("agent unverified = %#v", got.Unverified)
	}
	if len(got.Actionable) != 2 || got.Actionable[0].Kind != "regressed" || got.Actionable[1].Kind != "still_failing" {
		t.Fatalf("agent actionable = %#v", got.Actionable)
	}
	if got.Actionable[1].Message != "schema mismatch with detail" || got.Actionable[1].Operation != "GET /a" {
		t.Fatalf("agent problem projection = %#v", got.Actionable[1])
	}
	if got.Actionable[1].Status.Baseline == nil || *got.Actionable[1].Status.Baseline != 200 || got.Actionable[1].Status.Candidate == nil || *got.Actionable[1].Status.Candidate != 200 {
		t.Fatalf("agent status projection = %#v", got.Actionable[1].Status)
	}
	if got.Actionable[0].Count != 1 || got.Actionable[1].Count != 1 {
		t.Fatalf("agent counts per Problem Group = %#v", got.Actionable)
	}
	if !reflect.DeepEqual(got.Actionable[0].Refs, []int{1}) ||
		!reflect.DeepEqual(got.Actionable[1].Refs, []int{2}) {
		t.Fatalf("agent refs = %#v", got.Actionable)
	}
	if got.Actionable[0].Sample.Ref != 1 || got.Actionable[1].Sample.Ref != 2 {
		t.Fatalf("agent samples = %#v", got.Actionable)
	}
	if !reflect.DeepEqual(got, newAgentView(document)) {
		t.Fatal("agent view is not stable across projections")
	}
}

func TestNewAgentViewUsesEmptyActionableArrayWhenNoCasesRemain(t *testing.T) {
	view := newAgentView(report{})
	if view.SchemaVersion != "2" {
		t.Fatalf("empty view schema version = %q, want 2", view.SchemaVersion)
	}
	if view.Actionable == nil || len(view.Actionable) != 0 {
		t.Fatalf("empty Problem Group list = %#v, want non-nil empty list", view.Actionable)
	}

	contents, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode empty agent view: %v", err)
	}
	if !strings.Contains(string(contents), `"actionable":[]`) {
		t.Fatalf("empty agent view JSON = %s, want actionable []", contents)
	}
}

func TestNewAgentViewGroupsProblemGroupsByFullKey(t *testing.T) {
	firstRef := 1
	secondRef := 2
	thirdRef := 3
	fourthRef := 4
	fifthRef := 5
	document := report{
		Baseline:  reportCampaign{Campaign: "baseline"},
		Candidate: reportCandidate{Campaign: "candidate"},
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: 5},
			Traffic:          trafficSummary{Regressed: 1},
		},
		Problems: []baselineProblem{
			{
				CheckCategory: checkCategoryStatusCodeConformance,
				Message:       "same problem",
				CaseID:        "case-1",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &firstRef,
			},
			{
				CheckCategory: checkCategoryStatusCodeConformance,
				Message:       "same problem",
				CaseID:        "case-2",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &secondRef,
			},
			{
				CheckCategory: checkCategoryStatusCodeConformance,
				Message:       "same problem",
				CaseID:        "case-3",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &thirdRef,
			},
			{
				CheckCategory: checkCategoryStatusCodeConformance,
				Message:       "first message",
				CaseID:        "case-4a",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &fourthRef,
			},
			{
				CheckCategory: checkCategoryStatusCodeConformance,
				Message:       "second message",
				CaseID:        "case-4b",
				Outcome:       problemOutcomeStillFailing,
				Interaction:   &fourthRef,
			},
		},
		Findings: []reportInteractionEvidence{
			{
				Interaction:       fifthRef,
				Request:           reportRequest{Method: "GET", URL: "https://example.test/widgets/1"},
				Classification:    interactionClassificationRegressed,
				StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
				CandidateResponse: reportResponse{Status: 500},
			},
		},
		allInteractions: []reportInteractionEvidence{
			agentViewInteraction(1, "/widgets/1", 200, 500, "first", "first response"),
			agentViewInteraction(2, "/widgets/1", 200, 500, "second", "second response"),
			agentViewInteraction(3, "/widgets/1", 200, 501, "third", "third response"),
			agentViewInteraction(4, "/widgets/1", 200, 500, "fourth", "fourth response"),
			agentViewInteraction(5, "/widgets/1", 200, 500, "fifth", "fifth response"),
		},
	}

	got := newAgentView(document)
	if len(got.Actionable) != 5 {
		t.Fatalf("Problem Group count = %d, want 5: %#v", len(got.Actionable), got.Actionable)
	}
	if got.Actionable[0].Kind != agentreport.ActionKindRegressed {
		t.Fatalf("first Problem Group kind = %q, want regressed", got.Actionable[0].Kind)
	}

	sameProblem := findActionable(t, got.Actionable, agentreport.ActionKindStillFailing, "same problem")
	if sameProblem.Count != 2 || !reflect.DeepEqual(sameProblem.Refs, []int{1, 2}) {
		t.Fatalf("repeated Problem Group = %#v, want count 2 and refs [1 2]", sameProblem)
	}
	if sameProblem.Sample.Ref != 1 || sameProblem.Sample.RequestBody != "first" ||
		sameProblem.Sample.ResponseBody != "first response" {
		t.Fatalf("lowest-ref Problem Group sample = %#v", sameProblem.Sample)
	}

	differentStatus := findActionableWithCandidateStatus(
		t,
		got.Actionable,
		agentreport.ActionKindStillFailing,
		"same problem",
		501,
	)
	if differentStatus.Count != 1 || differentStatus.ID == sameProblem.ID {
		t.Fatalf("different status was merged into repeated Problem Group: %#v", differentStatus)
	}

	firstMessage := findActionable(t, got.Actionable, agentreport.ActionKindStillFailing, "first message")
	secondMessage := findActionable(t, got.Actionable, agentreport.ActionKindStillFailing, "second message")
	if firstMessage.ID == secondMessage.ID || firstMessage.Count != 1 || secondMessage.Count != 1 {
		t.Fatalf("same-case messages were not separate Problem Groups: %#v %#v", firstMessage, secondMessage)
	}
	if sameProblem.ID == got.Actionable[0].ID {
		t.Fatal("regression and still-failing Problem Groups share an id")
	}

	count := 0
	for _, actionable := range got.Actionable {
		count += actionable.Count
	}
	if count != got.Counts.StillFailing+got.Counts.Regressed {
		t.Fatalf("Problem Group count sum = %d, want %d", count, got.Counts.StillFailing+got.Counts.Regressed)
	}
}

func TestNewAgentViewUsesOpenAPIOperationTemplateWhenAvailable(t *testing.T) {
	ref := 1
	contract := mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Agent view contract
  version: "1.0"
paths:
  /api/v1/categories/{categoryId}/products:
    get:
      responses:
        "500":
          description: failure
`))
	document := report{
		Baseline:  reportCampaign{Campaign: "baseline"},
		Candidate: reportCandidate{Campaign: "candidate"},
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: 1},
		},
		Problems: []baselineProblem{{
			CheckCategory: checkCategoryStatusCodeConformance,
			Message:       "Undocumented status",
			Outcome:       problemOutcomeStillFailing,
			Interaction:   &ref,
		}},
		allInteractions: []reportInteractionEvidence{
			agentViewInteraction(1, "/api/v1/categories/0/products", 200, 500, "request", "response"),
		},
		schemaValidation: contract,
	}

	got := newAgentView(document)
	if len(got.Actionable) != 1 {
		t.Fatalf("Problem Groups = %#v, want one group", got.Actionable)
	}
	if got.Actionable[0].Operation != "GET /api/v1/categories/{categoryId}/products" {
		t.Fatalf("group operation = %q, want OpenAPI template", got.Actionable[0].Operation)
	}
	if got.Actionable[0].Sample.Operation != "GET /api/v1/categories/0/products" {
		t.Fatalf("sample operation = %q, want concrete operation", got.Actionable[0].Sample.Operation)
	}

	document.schemaValidation = nil
	got = newAgentView(document)
	if got.Actionable[0].Operation != "GET /api/v1/categories/0/products" {
		t.Fatalf("operation without contract = %q, want concrete operation", got.Actionable[0].Operation)
	}
	document.allInteractions[0].Request.URL = "https://example.test"
	got = newAgentView(document)
	if got.Actionable[0].Operation != "GET /" {
		t.Fatalf("root operation without contract = %q, want GET /", got.Actionable[0].Operation)
	}

	document.allInteractions[0].Request.URL = "https://example.test/api/v1/categories/0/products"
	document.schemaValidation = mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: No matching operation
  version: "1.0"
paths:
  /other:
    get:
      responses:
        "500":
          description: failure
`))
	got = newAgentView(document)
	if got.Actionable[0].Operation != "GET /api/v1/categories/0/products" {
		t.Fatalf("operation without matching template = %q, want concrete operation", got.Actionable[0].Operation)
	}
}

func TestNewAgentViewBoundsSampleBodiesAndDetails(t *testing.T) {
	ref := 1
	requestBody := strings.Repeat("r", agentEvidenceBodyCap+17)
	responseBody := strings.Repeat("s", agentEvidenceBodyCap+29)
	details := []string{
		"body.id: required",
		"body.name: required",
		"body.price: number",
		"body.stock: integer",
		"body.tags: array",
		"body.owner: string",
	}
	document := report{
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: 1},
		},
		Problems: []baselineProblem{{
			CheckCategory:          checkCategoryResponseSchemaConformance,
			Message:                "response schema mismatch",
			SchemaValidationErrors: details,
			Outcome:                problemOutcomeStillFailing,
			Interaction:            &ref,
		}},
		allInteractions: []reportInteractionEvidence{{
			Interaction: ref,
			Request: reportRequest{
				Method: "POST",
				URL:    "https://example.test/widgets",
				Body:   requestBody,
			},
			StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
			CandidateResponse: reportResponse{Status: 500, Body: responseBody},
		}},
	}

	got := newAgentView(document).Actionable[0].Sample
	requestMarker := "…[truncated 17 bytes]"
	responseMarker := "…[truncated 29 bytes]"
	if len(got.RequestBody) != agentEvidenceBodyCap+len(requestMarker) ||
		!strings.HasSuffix(got.RequestBody, requestMarker) ||
		!strings.HasPrefix(got.RequestBody, requestBody[:agentEvidenceBodyCap]) {
		t.Fatalf("bounded request body = %q", got.RequestBody)
	}
	if len(got.ResponseBody) != agentEvidenceBodyCap+len(responseMarker) ||
		!strings.HasSuffix(got.ResponseBody, responseMarker) ||
		!strings.HasPrefix(got.ResponseBody, responseBody[:agentEvidenceBodyCap]) {
		t.Fatalf("bounded response body = %q", got.ResponseBody)
	}
	if !reflect.DeepEqual(got.Details, details[:agentEvidenceDetailsCap]) {
		t.Fatalf("bounded schema details = %#v, want first five lines", got.Details)
	}

	document.Problems[0].CheckCategory = checkCategoryStatusCodeConformance
	document.Problems[0].SchemaValidationErrors = details
	got = newAgentView(document).Actionable[0].Sample
	if len(got.Details) != 0 {
		t.Fatalf("non-schema details = %#v, want empty array", got.Details)
	}
}

func TestNewAgentViewTruncatesSampleBodiesAtJSONSafeUTF8Boundary(t *testing.T) {
	requestBody := strings.Repeat("r", agentEvidenceBodyCap-1) + "é" + "request tail"
	responseBody := strings.Repeat("s", agentEvidenceBodyCap-1) + string([]byte{0xff}) + "response tail"
	document := report{
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: 1},
		},
		Problems: []baselineProblem{{
			CheckCategory: checkCategoryStatusCodeConformance,
			Message:       "response status mismatch",
			Outcome:       problemOutcomeStillFailing,
			Interaction:   intPointer(1),
		}},
		allInteractions: []reportInteractionEvidence{{
			Interaction: 1,
			Request: reportRequest{
				Method: "POST",
				URL:    "https://example.test/widgets",
				Body:   requestBody,
			},
			StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
			CandidateResponse: reportResponse{Status: 500, Body: responseBody},
		}},
	}

	sample := newAgentView(document).Actionable[0].Sample
	wantRequestBody := strings.Repeat("r", agentEvidenceBodyCap-1) + "…[truncated 14 bytes]"
	wantResponseBody := strings.Repeat("s", agentEvidenceBodyCap-1) + "…[truncated 16 bytes]"
	if sample.RequestBody != wantRequestBody || sample.ResponseBody != wantResponseBody {
		t.Fatalf("JSON-safe truncated bodies = %#v, want request %q and response %q", sample, wantRequestBody, wantResponseBody)
	}
	if !utf8.ValidString(sample.RequestBody) || !utf8.ValidString(sample.ResponseBody) {
		t.Fatalf("truncated bodies are not valid UTF-8: request %q, response %q", sample.RequestBody, sample.ResponseBody)
	}

	contents, err := json.Marshal(newAgentView(document))
	if err != nil {
		t.Fatalf("encode JSON-safe agent view: %v", err)
	}
	var decoded agentreport.View
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("decode JSON-safe agent view: %v", err)
	}
	decodedSample := decoded.Actionable[0].Sample
	if decodedSample.RequestBody != sample.RequestBody || decodedSample.ResponseBody != sample.ResponseBody {
		t.Fatalf("JSON changed truncated bodies: %#v, want %#v", decodedSample, sample)
	}
}

func TestNewAgentViewSizeIsBoundedForManyCasesInOneProblemGroup(t *testing.T) {
	const caseCount = 1000
	document := report{
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: caseCount},
		},
		Problems:        make([]baselineProblem, caseCount),
		allInteractions: make([]reportInteractionEvidence, caseCount),
	}
	for index := 0; index < caseCount; index++ {
		ref := index + 1
		document.Problems[index] = baselineProblem{
			CheckCategory: checkCategoryStatusCodeConformance,
			Message:       "same problem",
			Outcome:       problemOutcomeStillFailing,
			Interaction:   &ref,
		}
		document.allInteractions[index] = agentViewInteraction(
			ref,
			"/widgets/1",
			200,
			500,
			strings.Repeat("r", agentEvidenceBodyCap+1),
			strings.Repeat("s", agentEvidenceBodyCap+1),
		)
	}

	view := newAgentView(document)
	if len(view.Actionable) != 1 || view.Actionable[0].Count != caseCount ||
		!reflect.DeepEqual(view.Actionable[0].Refs, []int{1, 2, 3}) {
		t.Fatalf("many-case Problem Group = %#v", view.Actionable)
	}
	contents, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode many-case agent view: %v", err)
	}
	if len(contents) >= 100_000 {
		t.Fatalf("many-case agent view size = %d bytes, want under 100 KB", len(contents))
	}
}

func TestNewAgentViewProblemGroupIDIsStableAndKeyedByProblem(t *testing.T) {
	base := agentViewProblemReport("/widgets/1", checkCategoryStatusCodeConformance, "same", intPointer(200), 500)
	baseID := newAgentView(base).Actionable[0].ID
	if got := newAgentView(base).Actionable[0].ID; got != baseID {
		t.Fatalf("same Problem Group id changed across projections: %q and %q", baseID, got)
	}

	variants := []struct {
		name     string
		document report
	}{
		{
			name:     "operation",
			document: agentViewProblemReport("/widgets/2", checkCategoryStatusCodeConformance, "same", intPointer(200), 500),
		},
		{
			name:     "check category",
			document: agentViewProblemReport("/widgets/1", checkCategoryResponseSchemaConformance, "same", intPointer(200), 500),
		},
		{
			name:     "message",
			document: agentViewProblemReport("/widgets/1", checkCategoryStatusCodeConformance, "different", intPointer(200), 500),
		},
		{
			name:     "baseline status",
			document: agentViewProblemReport("/widgets/1", checkCategoryStatusCodeConformance, "same", nil, 500),
		},
		{
			name:     "candidate status",
			document: agentViewProblemReport("/widgets/1", checkCategoryStatusCodeConformance, "same", intPointer(200), 501),
		},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			variantID := newAgentView(variant.document).Actionable[0].ID
			if variantID == baseID {
				t.Fatalf("different Problem Group key part reused id %q", variantID)
			}
		})
	}

	regression := report{
		Summary: reportSummary{Traffic: trafficSummary{Regressed: 1}},
		Findings: []reportInteractionEvidence{
			{
				Interaction:       1,
				Request:           reportRequest{Method: "GET", URL: "https://example.test/widgets/1"},
				Classification:    interactionClassificationRegressed,
				StatusTransition:  statusTransition{Baseline: intPointer(200), Candidate: 500},
				CandidateResponse: reportResponse{Status: 500},
			},
		},
	}
	if regressionID := newAgentView(regression).Actionable[0].ID; regressionID == baseID {
		t.Fatalf("different Problem Group kind reused id %q", regressionID)
	}
}

func TestNewAgentViewOrdersProblemGroupsByKeyNotCount(t *testing.T) {
	refs := []int{1, 2, 3, 4, 5}
	contract := mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Ordering contract
  version: "1.0"
paths:
  /a/{id}:
    get:
      responses:
        "500":
          description: failure
  /z/{id}:
    get:
      responses:
        "400":
          description: failure
        "500":
          description: failure
`))
	document := report{
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: len(refs)},
		},
		Problems: []baselineProblem{
			{CheckCategory: checkCategoryStatusCodeConformance, Message: "z 500", Outcome: problemOutcomeStillFailing, Interaction: &refs[1]},
			{CheckCategory: checkCategoryStatusCodeConformance, Message: "z 500", Outcome: problemOutcomeStillFailing, Interaction: &refs[4]},
			{CheckCategory: checkCategoryStatusCodeConformance, Message: "a 500", Outcome: problemOutcomeStillFailing, Interaction: &refs[0]},
			{CheckCategory: checkCategoryResponseSchemaConformance, Message: "z schema", Outcome: problemOutcomeStillFailing, Interaction: &refs[3]},
			{CheckCategory: checkCategoryStatusCodeConformance, Message: "z 400", Outcome: problemOutcomeStillFailing, Interaction: &refs[2]},
		},
		allInteractions: []reportInteractionEvidence{
			agentViewInteraction(1, "/a/1", 200, 500, "", ""),
			agentViewInteraction(2, "/z/2", 200, 500, "", ""),
			agentViewInteraction(3, "/z/3", 200, 400, "", ""),
			agentViewInteraction(4, "/z/4", 200, 500, "", ""),
			agentViewInteraction(5, "/z/5", 200, 500, "", ""),
		},
		schemaValidation: contract,
	}

	got := newAgentView(document).Actionable
	if len(got) != 4 {
		t.Fatalf("Problem Groups = %d, want 4: %#v", len(got), got)
	}
	want := []struct {
		operation string
		category  agentreport.CheckCategory
		candidate int
		count     int
	}{
		{"GET /a/{id}", agentreport.CheckCategory(checkCategoryStatusCodeConformance), 500, 1},
		{"GET /z/{id}", agentreport.CheckCategory(checkCategoryResponseSchemaConformance), 500, 1},
		{"GET /z/{id}", agentreport.CheckCategory(checkCategoryStatusCodeConformance), 400, 1},
		{"GET /z/{id}", agentreport.CheckCategory(checkCategoryStatusCodeConformance), 500, 2},
	}
	for index, expected := range want {
		if got[index].Operation != expected.operation || got[index].CheckCategory != expected.category ||
			got[index].Count != expected.count || got[index].Status.Candidate == nil ||
			*got[index].Status.Candidate != expected.candidate {
			t.Fatalf("Problem Group %d = %#v, want operation %q, category %q, candidate %d, count %d", index, got[index], expected.operation, expected.category, expected.candidate, expected.count)
		}
	}
}

func agentViewProblemReport(
	path string,
	category checkCategory,
	message string,
	baselineStatus *int,
	candidateStatus int,
) report {
	ref := 1
	return report{
		Summary: reportSummary{
			BaselineProblems: baselineProblemSummary{StillFailing: 1},
		},
		Problems: []baselineProblem{{
			CheckCategory: category,
			Message:       message,
			Outcome:       problemOutcomeStillFailing,
			Interaction:   &ref,
		}},
		allInteractions: []reportInteractionEvidence{{
			Interaction: ref,
			Request: reportRequest{
				Method: "GET",
				URL:    "https://example.test" + path,
			},
			StatusTransition: statusTransition{
				Baseline:  baselineStatus,
				Candidate: candidateStatus,
			},
			CandidateResponse: reportResponse{Status: candidateStatus},
		}},
	}
}

func agentViewInteraction(
	ref int,
	path string,
	baselineStatus int,
	candidateStatus int,
	requestBody string,
	responseBody string,
) reportInteractionEvidence {
	return reportInteractionEvidence{
		Interaction: ref,
		Request: reportRequest{
			Method: "GET",
			URL:    "https://example.test" + path,
			Body:   requestBody,
		},
		StatusTransition:  statusTransition{Baseline: intPointer(baselineStatus), Candidate: candidateStatus},
		CandidateResponse: reportResponse{Status: candidateStatus, Body: responseBody},
	}
}

func findActionable(
	t *testing.T,
	actionable []agentreport.Actionable,
	kind agentreport.ActionKind,
	message string,
) agentreport.Actionable {
	t.Helper()
	for _, item := range actionable {
		if item.Kind == kind && item.Message == message {
			return item
		}
	}

	t.Fatalf("Problem Group %q/%q not found in %#v", kind, message, actionable)
	return agentreport.Actionable{}
}

func findActionableWithCandidateStatus(
	t *testing.T,
	actionable []agentreport.Actionable,
	kind agentreport.ActionKind,
	message string,
	candidateStatus int,
) agentreport.Actionable {
	t.Helper()
	for _, item := range actionable {
		if item.Kind != kind || item.Message != message || item.Status.Candidate == nil {
			continue
		}
		if *item.Status.Candidate == candidateStatus {
			return item
		}
	}

	t.Fatalf(
		"Problem Group %q/%q with candidate status %d not found in %#v",
		kind,
		message,
		candidateStatus,
		actionable,
	)
	return agentreport.Actionable{}
}
