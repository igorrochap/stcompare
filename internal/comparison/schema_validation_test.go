package comparison

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewReportClassifiesResponseSchemaProblemWithContractValidation(t *testing.T) {
	tests := []struct {
		name       string
		requestURL string
		headers    []harHeader
		body       string
		want       schemaProblemOutcome
	}{
		{
			name:       "same status valid response fixed",
			requestURL: "https://baseline.example.test/widgets/123",
			headers:    []harHeader{{Name: "content-type", Value: "application/json; charset=utf-8"}},
			body:       `{"name":"Ada"}`,
			want: schemaProblemOutcome{
				Outcome: problemOutcomeFixed,
				Reason:  problemOutcomeReasonSchemaValidationPassed,
				Fixed:   1,
			},
		},
		{
			name:       "same status invalid response still failing",
			requestURL: "https://baseline.example.test/widgets/123",
			headers:    []harHeader{{Name: "Content-Type", Value: "application/json"}},
			body:       `{"id":"123"}`,
			want: schemaProblemOutcome{
				Outcome:          problemOutcomeStillFailing,
				Reason:           problemOutcomeReasonRepeatedSchemaViolation,
				StillFailing:     1,
				ValidationErrors: []string{"body.name: property \"name\" is missing"},
			},
		},
		{
			name:       "missing operation",
			requestURL: "https://baseline.example.test/unknown",
			headers:    []harHeader{{Name: "Content-Type", Value: "application/json"}},
			body:       `{"name":"Ada"}`,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeInconclusive,
				Reason:       problemOutcomeReasonSchemaOperationNotFound,
				Inconclusive: 1,
			},
		},
		{
			name:       "unsupported media type",
			requestURL: "https://baseline.example.test/widgets/123",
			headers:    []harHeader{{Name: "Content-Type", Value: "application/xml"}},
			body:       `<widget/>`,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeInconclusive,
				Reason:       problemOutcomeReasonSchemaMediaTypeUnsupported,
				Inconclusive: 1,
			},
		},
		{
			name:       "unparseable body",
			requestURL: "https://baseline.example.test/widgets/123",
			headers:    []harHeader{{Name: "Content-Type", Value: "application/json"}},
			body:       `{"name":`,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeInconclusive,
				Reason:       problemOutcomeReasonSchemaResponseBodyUnparseable,
				Inconclusive: 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newSchemaValidationReport(t, schemaValidationReportInput{
				RequestURL: test.requestURL,
				Headers:    test.headers,
				Body:       test.body,
			})

			got := schemaProblemOutcome{
				Outcome:          document.Problems[0].Outcome,
				Reason:           document.Problems[0].OutcomeReason,
				ValidationErrors: document.Problems[0].SchemaValidationErrors,
				Fixed:            document.Summary.BaselineProblems.Fixed,
				StillFailing:     document.Summary.BaselineProblems.StillFailing,
				Inconclusive:     document.Summary.BaselineProblems.Inconclusive,
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("schema problem outcome = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNewReportUsesRawCandidateBodyForSchemaValidation(t *testing.T) {
	document := newSchemaValidationReport(t, schemaValidationReportInput{
		RequestURL: "https://baseline.example.test/widgets/123",
		Headers:    []harHeader{{Name: "Content-Type", Value: "application/json"}},
		Body:       `{"id":"generated-1"}`,
		Normalization: ResponseNormalizationConfig{
			BodyFields: []BodyFieldNormalizationRule{
				{Name: "generated-name", FieldName: "id"},
			},
		},
	})

	got := schemaProblemOutcome{
		Outcome:          document.Problems[0].Outcome,
		Reason:           document.Problems[0].OutcomeReason,
		ValidationErrors: document.Problems[0].SchemaValidationErrors,
		StillFailing:     document.Summary.BaselineProblems.StillFailing,
	}
	want := schemaProblemOutcome{
		Outcome:          problemOutcomeStillFailing,
		Reason:           problemOutcomeReasonRepeatedSchemaViolation,
		ValidationErrors: []string{"body.name: property \"name\" is missing"},
		StillFailing:     1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema validation with normalization = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsSchemaValidStatusChangeInconclusive(t *testing.T) {
	document := newSchemaValidationReport(t, schemaValidationReportInput{
		RequestURL:       "https://baseline.example.test/widgets/123",
		CandidateStatus:  404,
		Headers:          []harHeader{{Name: "Content-Type", Value: "application/json"}},
		Body:             `{"name":"Ada"}`,
		PreconditionRule: NewPreconditionHeuristic("generated-widget", "POST", `^/widgets/123$`),
	})

	got := schemaProblemOutcome{
		Outcome:      document.Problems[0].Outcome,
		Reason:       document.Problems[0].OutcomeReason,
		Inconclusive: document.Summary.BaselineProblems.Inconclusive,
	}
	want := schemaProblemOutcome{
		Outcome:      problemOutcomeInconclusive,
		Reason:       problemOutcomeReasonGeneratedResourcePreconditionLoss,
		Inconclusive: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema-valid status change = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesStatusCodeConformanceProblemWithContract(t *testing.T) {
	tests := []struct {
		name            string
		contract        *OpenAPIContract
		requestURL      string
		candidateStatus int
		want            schemaProblemOutcome
	}{
		{
			name:            "explicit documented status fixed",
			contract:        statusCodeConformanceTestContract(t),
			requestURL:      "https://baseline.example.test/widgets/123",
			candidateStatus: 201,
			want: schemaProblemOutcome{
				Outcome: problemOutcomeFixed,
				Reason:  problemOutcomeReasonStatusCodeDocumented,
				Fixed:   1,
			},
		},
		{
			name:            "range documented status fixed",
			contract:        statusCodeConformanceTestContract(t),
			requestURL:      "https://baseline.example.test/widgets/123",
			candidateStatus: 204,
			want: schemaProblemOutcome{
				Outcome: problemOutcomeFixed,
				Reason:  problemOutcomeReasonStatusCodeDocumented,
				Fixed:   1,
			},
		},
		{
			name:            "default does not document status",
			contract:        statusCodeConformanceTestContract(t),
			requestURL:      "https://baseline.example.test/widgets/123",
			candidateStatus: 418,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeStillFailing,
				Reason:       problemOutcomeReasonStatusCodeUndocumented,
				StillFailing: 1,
			},
		},
		{
			name:            "missing contract inconclusive",
			contract:        nil,
			requestURL:      "https://baseline.example.test/widgets/123",
			candidateStatus: 201,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeInconclusive,
				Reason:       problemOutcomeReasonSchemaContractUnavailable,
				Inconclusive: 1,
			},
		},
		{
			name:            "operation not found inconclusive",
			contract:        statusCodeConformanceTestContract(t),
			requestURL:      "https://baseline.example.test/unknown",
			candidateStatus: 201,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeInconclusive,
				Reason:       problemOutcomeReasonSchemaOperationNotFound,
				Inconclusive: 1,
			},
		},
		{
			name:            "operation ambiguous inconclusive",
			contract:        ambiguousStatusCodeConformanceTestContract(t),
			requestURL:      "https://baseline.example.test/widgets/123",
			candidateStatus: 201,
			want: schemaProblemOutcome{
				Outcome:      problemOutcomeInconclusive,
				Reason:       problemOutcomeReasonSchemaOperationAmbiguous,
				Inconclusive: 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newStatusCodeConformanceReport(t, statusCodeConformanceReportInput{
				Contract:        test.contract,
				RequestURL:      test.requestURL,
				CandidateStatus: test.candidateStatus,
			})

			got := schemaProblemOutcome{
				Outcome:      document.Problems[0].Outcome,
				Reason:       document.Problems[0].OutcomeReason,
				Fixed:        document.Summary.BaselineProblems.Fixed,
				StillFailing: document.Summary.BaselineProblems.StillFailing,
				Inconclusive: document.Summary.BaselineProblems.Inconclusive,
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("status code conformance outcome = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestOpenAPIContractValidationReportsDistinctInconclusiveReasons(t *testing.T) {
	tests := []struct {
		name     string
		contract *OpenAPIContract
		request  schemaValidationRequest
		want     problemOutcomeReason
	}{
		{
			name:     "missing contract",
			contract: nil,
			request:  validSchemaValidationRequest(),
			want:     problemOutcomeReasonSchemaContractUnavailable,
		},
		{
			name:     "unsupported OpenAPI version",
			contract: loadOpenAPIContract([]byte(`swagger: "2.0"`)),
			request:  validSchemaValidationRequest(),
			want:     problemOutcomeReasonSchemaOpenAPIVersionUnsupported,
		},
		{
			name: "ambiguous operation",
			contract: mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Ambiguous
  version: "1.0"
paths:
  /widgets/{left}:
    parameters:
      - name: left
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
  /widgets/{right}:
    parameters:
      - name: right
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
`)),
			request: validSchemaValidationRequest(),
			want:    problemOutcomeReasonSchemaOperationAmbiguous,
		},
		{
			name: "missing schema for status",
			contract: mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Missing response
  version: "1.0"
paths:
  /widgets/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "201":
          description: response
          content:
            application/json:
              schema:
                type: object
`)),
			request: validSchemaValidationRequest(),
			want:    problemOutcomeReasonSchemaResponseSchemaMissing,
		},
		{
			name:     "missing media type header",
			contract: schemaValidationTestContract(t),
			request: schemaValidationRequest{
				Method: "GET",
				URL:    "https://baseline.example.test/widgets/123",
				Response: reportResponse{
					Status: 200,
					Body:   `{}`,
				},
			},
			want: problemOutcomeReasonSchemaMediaTypeMissing,
		},
		{
			name:     "missing response body",
			contract: schemaValidationTestContract(t),
			request: schemaValidationRequest{
				Method: "GET",
				URL:    "https://baseline.example.test/widgets/123",
				Response: reportResponse{
					Status:  200,
					Headers: []harHeader{{Name: "Content-Type", Value: "application/json"}},
				},
			},
			want: problemOutcomeReasonSchemaResponseBodyUnavailable,
		},
		{
			name: "unresolved top-level reference",
			contract: loadOpenAPIContract([]byte(`
openapi: 3.0.3
info:
  title: Bad ref
  version: "1.0"
paths:
  /widgets/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Missing"
`)),
			request: validSchemaValidationRequest(),
			want:    problemOutcomeReasonSchemaReferenceUnresolved,
		},
		{
			name: "unresolved nested reference",
			contract: loadOpenAPIContract([]byte(`
openapi: 3.0.3
info:
  title: Bad nested ref
  version: "1.0"
paths:
  /widgets/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
                properties:
                  owner:
                    $ref: "#/components/schemas/Missing"
`)),
			request: validSchemaValidationRequest(),
			want:    problemOutcomeReasonSchemaReferenceUnresolved,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.contract.Validate(test.request).OutcomeReason
			if got != test.want {
				t.Fatalf("validation reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLoadOpenAPIContractReportsUnreadableContract(t *testing.T) {
	contract := LoadOpenAPIContract(filepath.Join(t.TempDir(), "missing.yaml"))

	got := contract.Validate(validSchemaValidationRequest()).OutcomeReason
	want := problemOutcomeReasonSchemaContractUnreadable
	if got != want {
		t.Fatalf("unreadable contract reason = %q, want %q", got, want)
	}
}

func TestNewReportIncludesSchemaValidationProvenanceInJSON(t *testing.T) {
	document := newReport(reportInput{
		SchemaValidation: schemaValidationTestContract(t),
	})

	contents, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	var got struct {
		SchemaValidation schemaValidationProvenance `json:"schema_validation"`
	}
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}

	want := schemaValidationProvenance{
		Validator:                       "github.com/getkin/kin-openapi/openapi3filter",
		ValidatorVersion:                "v0.145.0",
		SupportedOpenAPIVersions:        "3.0, 3.1, 3.2",
		SupportedJSONSchemaDialects:     "OpenAPI 3.0 Schema Object; JSON Schema 2020-12 for OpenAPI 3.1+",
		OpenAPIVersion:                  "3.0.3",
		StatusCodeConformanceDefinition: "explicit status codes and range codes such as 2XX count as documented; default responses do not document every status",
		ResponseValidationUsesRawBody:   true,
		ResponseMediaTypeSource:         "candidate_response_headers",
		OperationResolutionTieBehavior:  "literal_segments_then_parameter_count; equal-specificity matches are inconclusive",
	}
	if !reflect.DeepEqual(got.SchemaValidation, want) {
		t.Fatalf("schema validation provenance = %#v, want %#v", got.SchemaValidation, want)
	}
}

func TestOpenAPIContractResolvesMostSpecificOperationAndEncodedSegments(t *testing.T) {
	contract := mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Routing
  version: "1.0"
paths:
  /widgets/search:
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
                required: [literal]
                properties:
                  literal:
                    type: string
  /widgets/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
                required: [name]
                properties:
                  name:
                    type: string
`))

	tests := []struct {
		name string
		url  string
		body string
	}{
		{
			name: "literal path beats parameter path",
			url:  "https://baseline.example.test/widgets/search",
			body: `{"literal":"yes"}`,
		},
		{
			name: "encoded slash remains one parameter segment",
			url:  "https://candidate.example.test/widgets/a%2Fb?expand=true",
			body: `{"name":"Ada"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validSchemaValidationRequest()
			request.URL = test.url
			request.Response.Body = test.body

			got := contract.Validate(request).OutcomeReason
			want := problemOutcomeReasonSchemaValidationPassed
			if got != want {
				t.Fatalf("operation resolution validation reason = %q, want %q", got, want)
			}
		})
	}
}

func TestPrepareComparisonLoadsConfiguredOpenAPIContract(t *testing.T) {
	prepared, err := prepareComparison(Input{
		SchemaPath:       filepath.Join("testdata", "widgets-openapi.yaml"),
		BaselineHARPath:  filepath.Join("testdata", "schemathesis-matched-real.har.json"),
		BaselineVCRPath:  filepath.Join(t.TempDir(), "missing.vcr.yaml"),
		CandidateBaseURL: "https://candidate.example.test",
	})
	if err != nil {
		t.Fatalf("prepareComparison returned error: %v", err)
	}
	if len(prepared.baselineEntries) < 2 || prepared.baselineEntries[1].Response == nil {
		t.Fatal("real HAR fixture does not contain the expected recorded POST response")
	}
	recorded := prepared.baselineEntries[1]

	result := prepared.schemaValidation.Validate(schemaValidationRequest{
		Method: recorded.Request.Method,
		URL:    recorded.Request.URL,
		Response: reportResponse{
			Status:  recorded.Response.Status,
			Headers: recorded.Response.Headers,
			Body:    recorded.Response.Content.Text,
		},
	})
	if result.OutcomeReason != problemOutcomeReasonRepeatedSchemaViolation {
		t.Fatalf("real OpenAPI schema validation = %#v", result)
	}
	if len(result.Errors) == 0 {
		t.Fatal("real OpenAPI schema validation did not record validation errors")
	}
}

func validSchemaValidationRequest() schemaValidationRequest {
	return schemaValidationRequest{
		Method: "GET",
		URL:    "https://baseline.example.test/widgets/123",
		Response: reportResponse{
			Status:  200,
			Headers: []harHeader{{Name: "Content-Type", Value: "application/json"}},
			Body:    `{}`,
		},
	}
}

type schemaProblemOutcome struct {
	Outcome          problemOutcome
	Reason           problemOutcomeReason
	ValidationErrors []string
	Fixed            int
	StillFailing     int
	Inconclusive     int
}

type schemaValidationReportInput struct {
	RequestURL       string
	CandidateStatus  int
	Headers          []harHeader
	Body             string
	Normalization    ResponseNormalizationConfig
	PreconditionRule PreconditionHeuristic
}

type statusCodeConformanceReportInput struct {
	Contract        *OpenAPIContract
	RequestURL      string
	CandidateStatus int
}

func newStatusCodeConformanceReport(
	t *testing.T,
	input statusCodeConformanceReportInput,
) report {
	t.Helper()

	interaction := 1
	return newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "status_code_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-status",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    input.RequestURL,
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request:  harRequest{Method: "GET", URL: input.RequestURL},
					Response: &harResponse{Status: 418},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{Status: input.CandidateStatus},
					},
				},
			},
		},
		SchemaValidation: input.Contract,
	})
}

func newSchemaValidationReport(
	t *testing.T,
	input schemaValidationReportInput,
) report {
	t.Helper()

	status := input.CandidateStatus
	if status == 0 {
		status = 200
	}
	interaction := 1
	policy := PreconditionPolicy{Normalization: input.Normalization}
	if input.PreconditionRule.Name != "" {
		policy.MissingResourceStatuses = []int{404}
		policy.Heuristics = []PreconditionHeuristic{input.PreconditionRule}
	}

	return newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-schema",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "POST",
						URL:    input.RequestURL,
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "POST",
						URL:    input.RequestURL,
					},
					Response: &harResponse{Status: 200},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  status,
							Headers: input.Headers,
							Content: harContent{Text: input.Body},
						},
					},
				},
			},
		},
		PreconditionPolicy: policy,
		SchemaValidation:   schemaValidationTestContract(t),
	})
}

func statusCodeConformanceTestContract(t *testing.T) *OpenAPIContract {
	t.Helper()

	return mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Status codes
  version: "1.0"
paths:
  /widgets/{widget_id}:
    parameters:
      - name: widget_id
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "201":
          description: created
        2XX:
          description: any successful response
        default:
          description: fallback
`))
}

func ambiguousStatusCodeConformanceTestContract(t *testing.T) *OpenAPIContract {
	t.Helper()

	return mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Ambiguous status codes
  version: "1.0"
paths:
  /widgets/{left}:
    parameters:
      - name: left
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "201":
          description: created
  /widgets/{right}:
    parameters:
      - name: right
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "201":
          description: created
`))
}

func schemaValidationTestContract(t *testing.T) *OpenAPIContract {
	t.Helper()

	return mustLoadOpenAPIContract(t, []byte(`
openapi: 3.0.3
info:
  title: Widgets
  version: "1.0"
paths:
  /widgets/{widget_id}:
    parameters:
      - name: widget_id
        in: path
        required: true
        schema:
          type: string
    get:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
    post:
      responses:
        "200":
          description: response
          content:
            application/json:
              schema:
                type: object
                required: [name]
                properties:
                  name:
                    type: string
`))
}

func mustLoadOpenAPIContract(t *testing.T, contents []byte) *OpenAPIContract {
	t.Helper()

	contract := loadOpenAPIContract(contents)
	if contract.limitation != "" {
		t.Fatalf(
			"loadOpenAPIContract limitation = %q: %s",
			contract.limitation,
			contract.loadMessage,
		)
	}

	return contract
}
