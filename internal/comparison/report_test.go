package comparison

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewReportPreservesUncorrelatedProblemWhenEvidenceAvailable(t *testing.T) {
	problem := baselineProblem{
		CheckName:         "status_code_conformance",
		Message:           "Received an undocumented status code: 418",
		EvidenceSource:    "vcr",
		CaseID:            "case-missing-from-har",
		CorrelationStatus: correlationStatusUncorrelated,
		Reproduction: problemReproduction{
			Method: "POST",
			URL:    "https://baseline.example.test/widgets",
			Headers: []harHeader{
				{Name: "Content-Type", Value: "application/json"},
			},
			Body: `{"name":"Ada"}`,
		},
	}

	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems:  []baselineProblem{problem},
		},
	})

	got := struct {
		Available bool
		Note      string
		Problems  []baselineProblem
	}{
		Available: document.BaselineProblemsAvailable,
		Note:      document.BaselineProblemsNote,
		Problems:  document.Problems,
	}
	want := struct {
		Available bool
		Note      string
		Problems  []baselineProblem
	}{
		Available: true,
		Problems: []baselineProblem{
			func() baselineProblem {
				problem.CheckCategory = checkCategoryUncategorized
				return problem
			}(),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport problem state = %#v, want %#v", got, want)
	}
}

func TestNewReportRepresentsAvailableZeroProblemEvidence(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
	})

	got := struct {
		Available bool
		Problems  []baselineProblem
	}{
		Available: document.BaselineProblemsAvailable,
		Problems:  document.Problems,
	}
	want := struct {
		Available bool
		Problems  []baselineProblem
	}{
		Available: true,
		Problems:  []baselineProblem{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport zero-problem state = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsJUnitProblemCountAndAddsStructuredExtractedCount(t *testing.T) {
	legacyCount := 1
	legacySource := "junit.xml"
	document := newReport(reportInput{
		BaselineProblemCount:       &legacyCount,
		BaselineProblemCountSource: &legacySource,
		BaselineProblemEvidence: baselineProblemEvidence{
			Available:  true,
			SourcePath: "campaign.vcr.yaml",
			Problems: []baselineProblem{
				{EvidenceSource: "vcr", CaseID: "case-1"},
				{EvidenceSource: "vcr", CaseID: "case-2"},
			},
		},
	})

	got := struct {
		ProblemCount                *int
		ProblemCountSource          *string
		ExtractedProblemCount       *int
		ExtractedProblemCountSource *string
	}{
		ProblemCount:                document.Baseline.ProblemCount,
		ProblemCountSource:          document.Baseline.ProblemCountSource,
		ExtractedProblemCount:       document.Baseline.ExtractedProblemCount,
		ExtractedProblemCountSource: document.Baseline.ExtractedProblemCountSource,
	}
	structuredCount := 2
	structuredSource := "campaign.vcr.yaml"
	want := struct {
		ProblemCount                *int
		ProblemCountSource          *string
		ExtractedProblemCount       *int
		ExtractedProblemCountSource *string
	}{
		ProblemCount:                &legacyCount,
		ProblemCountSource:          &legacySource,
		ExtractedProblemCount:       &structuredCount,
		ExtractedProblemCountSource: &structuredSource,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport baseline problem summary = %#v, want %#v", got, want)
	}
}

func TestNewReportUsesCorrelatedProblemSchemaVersion(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					EvidenceSource:    "vcr",
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
	})

	if document.SchemaVersion != "6" {
		t.Fatalf("newReport schema version = %q, want %q", document.SchemaVersion, "6")
	}
}

func TestNewReportCategorizesRealSchemathesisFixtureProblems(t *testing.T) {
	parsed, err := readVCRProblems(filepath.Join("testdata", "schemathesis-real.vcr.yaml"))
	if err != nil {
		t.Fatalf("readVCRProblems returned error: %v", err)
	}

	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems:  parsed.Problems,
		},
	})

	type categorizedCheck struct {
		Name     string
		Category checkCategory
	}
	got := make([]categorizedCheck, 0, len(document.Problems))
	for _, problem := range document.Problems {
		got = append(got, categorizedCheck{
			Name:     problem.CheckName,
			Category: problem.CheckCategory,
		})
	}
	want := []categorizedCheck{
		{Name: "not_a_server_error", Category: checkCategoryServerError},
		{Name: "unsupported_method", Category: checkCategoryUncategorized},
		{Name: "response_schema_conformance", Category: checkCategoryResponseSchemaConformance},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport real fixture categories = %#v, want %#v", got, want)
	}
}

func TestNewReportIncludesPreconditionPolicyProvenanceInJSON(t *testing.T) {
	document := newReport(reportInput{
		PreconditionPolicy: PreconditionPolicy{
			MissingResourceStatuses: []int{403, 404},
			Heuristics: []PreconditionHeuristic{
				NewPreconditionHeuristic(
					"generated-widget",
					"GET",
					`^/widgets/[0-9a-f]+$`,
				),
				NewPreconditionHeuristic(
					"deleted-account",
					"DELETE",
					`^/accounts/[0-9]+$`,
				),
			},
			Normalization: ResponseNormalizationConfig{
				BodyFields: []BodyFieldNormalizationRule{
					{Name: "generated-id", FieldName: "id"},
				},
			},
		},
	})

	contents, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	type heuristicProjection struct {
		Name        string `json:"name"`
		Method      string `json:"method"`
		PathPattern string `json:"path_pattern"`
	}
	type reportProjection struct {
		SchemaVersion string `json:"schema_version"`
		Comparison    struct {
			MissingResourceStatuses []int                 `json:"missing_resource_statuses"`
			PreconditionHeuristics  []heuristicProjection `json:"precondition_heuristics"`
			Normalization           struct {
				BodyFields []struct {
					Name      string `json:"name"`
					FieldName string `json:"field_name"`
				} `json:"body_fields"`
			} `json:"normalization"`
		} `json:"comparison"`
	}
	var got reportProjection
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode report projection: %v", err)
	}
	want := reportProjection{
		SchemaVersion: "6",
		Comparison: struct {
			MissingResourceStatuses []int                 `json:"missing_resource_statuses"`
			PreconditionHeuristics  []heuristicProjection `json:"precondition_heuristics"`
			Normalization           struct {
				BodyFields []struct {
					Name      string `json:"name"`
					FieldName string `json:"field_name"`
				} `json:"body_fields"`
			} `json:"normalization"`
		}{
			MissingResourceStatuses: []int{403, 404},
			PreconditionHeuristics: []heuristicProjection{
				{
					Name:        "generated-widget",
					Method:      "GET",
					PathPattern: `^/widgets/[0-9a-f]+$`,
				},
				{
					Name:        "deleted-account",
					Method:      "DELETE",
					PathPattern: `^/accounts/[0-9]+$`,
				},
			},
			Normalization: struct {
				BodyFields []struct {
					Name      string `json:"name"`
					FieldName string `json:"field_name"`
				} `json:"body_fields"`
			}{
				BodyFields: []struct {
					Name      string `json:"name"`
					FieldName string `json:"field_name"`
				}{
					{Name: "generated-id", FieldName: "id"},
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport comparison provenance = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCorrelatedServerErrorProblemStillFailing(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Response: &harResponse{Status: 500},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{Status: 502},
					},
				},
			},
		},
	})

	got := struct {
		Outcome              problemOutcome
		TrafficClass         interactionClassification
		StillFailingProblems int
	}{
		Outcome:              document.Problems[0].Outcome,
		TrafficClass:         document.Findings[0].Classification,
		StillFailingProblems: document.Summary.BaselineProblems.StillFailing,
	}
	want := struct {
		Outcome              problemOutcome
		TrafficClass         interactionClassification
		StillFailingProblems int
	}{
		Outcome:              problemOutcomeStillFailing,
		TrafficClass:         interactionClassificationChanged,
		StillFailingProblems: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport classification = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsCorrelatedProblemVisibleWhenStatusIsUnchanged(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 500}},
				},
			},
		},
	})

	got := struct {
		ProblemCount    int
		Outcome         problemOutcome
		TrafficFindings int
		ChangedTraffic  int
		HealthyTraffic  int
	}{
		ProblemCount:    len(document.Problems),
		Outcome:         document.Problems[0].Outcome,
		TrafficFindings: len(document.Findings),
		ChangedTraffic:  document.Summary.Traffic.Changed,
		HealthyTraffic:  document.Summary.Traffic.SuccessUnchanged,
	}
	want := struct {
		ProblemCount    int
		Outcome         problemOutcome
		TrafficFindings int
		ChangedTraffic  int
		HealthyTraffic  int
	}{
		ProblemCount:    1,
		Outcome:         problemOutcomeStillFailing,
		TrafficFindings: 0,
		ChangedTraffic:  1,
		HealthyTraffic:  0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport unchanged status problem visibility = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesPersistentServerErrorWithoutProblemAsChanged(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 500}},
				},
			},
		},
	})

	got := struct {
		FindingCount   int
		Classification interactionClassification
		ChangedTraffic int
		HealthyTraffic int
	}{
		FindingCount:   len(document.Findings),
		Classification: document.Findings[0].Classification,
		ChangedTraffic: document.Summary.Traffic.Changed,
		HealthyTraffic: document.Summary.Traffic.SuccessUnchanged,
	}
	want := struct {
		FindingCount   int
		Classification interactionClassification
		ChangedTraffic int
		HealthyTraffic int
	}{
		FindingCount:   1,
		Classification: interactionClassificationChanged,
		ChangedTraffic: 1,
		HealthyTraffic: 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport persistent server error traffic = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCorrelatedServerErrorProblemInconclusive(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 200}},
				},
			},
		},
	})

	got := struct {
		Outcome              problemOutcome
		InconclusiveProblems int
		FixedProblems        int
	}{
		Outcome:              document.Problems[0].Outcome,
		InconclusiveProblems: document.Summary.BaselineProblems.Inconclusive,
		FixedProblems:        document.Summary.BaselineProblems.Fixed,
	}
	want := struct {
		Outcome              problemOutcome
		InconclusiveProblems int
		FixedProblems        int
	}{
		Outcome:              problemOutcomeInconclusive,
		InconclusiveProblems: 1,
		FixedProblems:        0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport inconclusive classification = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCorrelatedServerErrorProblemFixedWithExerciseEvidence(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
					Response: &harResponse{
						Status: 500,
						Content: harContent{
							Text: `{"error":"boom","resource":{"name":"Ada"}}`,
						},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status: 200,
							Content: harContent{
								Text: `{"error":"boom","resource":{"name":"Ada"}}`,
							},
						},
					},
				},
			},
		},
	})

	got := struct {
		Outcome          problemOutcome
		OutcomeReason    problemOutcomeReason
		ExerciseEvidence []string
		Evaluable        int
		Fixed            int
		Inconclusive     int
	}{
		Outcome:          document.Problems[0].Outcome,
		OutcomeReason:    document.Problems[0].OutcomeReason,
		ExerciseEvidence: document.Problems[0].ExerciseEvidence,
		Evaluable:        document.Summary.BaselineProblems.Evaluable,
		Fixed:            document.Summary.BaselineProblems.Fixed,
		Inconclusive:     document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		Outcome          problemOutcome
		OutcomeReason    problemOutcomeReason
		ExerciseEvidence []string
		Evaluable        int
		Fixed            int
		Inconclusive     int
	}{
		Outcome: problemOutcomeFixed,
		ExerciseEvidence: []string{
			"operation_and_path_match",
			"normalized_response_body_match",
			"no_precondition_loss_detected",
		},
		Evaluable: 1,
		Fixed:     1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport fixed exercise evidence = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsCorrelatedServerErrorProblemInconclusiveWithoutExerciseEvidence(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
					Response: &harResponse{
						Status:  500,
						Content: harContent{Text: `{"error":"boom"}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  200,
							Content: harContent{Text: `{"resource":{"name":"Ada"}}`},
						},
					},
				},
			},
		},
	})

	got := struct {
		Outcome          problemOutcome
		OutcomeReason    problemOutcomeReason
		ExerciseEvidence []string
		Evaluable        int
		Fixed            int
		Inconclusive     int
	}{
		Outcome:          document.Problems[0].Outcome,
		OutcomeReason:    document.Problems[0].OutcomeReason,
		ExerciseEvidence: document.Problems[0].ExerciseEvidence,
		Evaluable:        document.Summary.BaselineProblems.Evaluable,
		Fixed:            document.Summary.BaselineProblems.Fixed,
		Inconclusive:     document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		Outcome          problemOutcome
		OutcomeReason    problemOutcomeReason
		ExerciseEvidence []string
		Evaluable        int
		Fixed            int
		Inconclusive     int
	}{
		Outcome:       problemOutcomeInconclusive,
		OutcomeReason: problemOutcomeReasonExerciseEvidenceMissing,
		ExerciseEvidence: []string{
			"operation_and_path_match",
			"no_precondition_loss_detected",
		},
		Evaluable:    1,
		Inconclusive: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport missing exercise evidence = %#v, want %#v", got, want)
	}
}

func TestNewReportDoesNotUseAbsentReproductionDataAsOperationEvidence(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					Message:           "Server error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
					Response: &harResponse{
						Status:  500,
						Content: harContent{Text: `{"error":"boom"}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  200,
							Content: harContent{Text: `{"error":"boom"}`},
						},
					},
				},
			},
		},
	})

	got := struct {
		Outcome          problemOutcome
		OutcomeReason    problemOutcomeReason
		ExerciseEvidence []string
		Fixed            int
		Inconclusive     int
	}{
		Outcome:          document.Problems[0].Outcome,
		OutcomeReason:    document.Problems[0].OutcomeReason,
		ExerciseEvidence: document.Problems[0].ExerciseEvidence,
		Fixed:            document.Summary.BaselineProblems.Fixed,
		Inconclusive:     document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		Outcome          problemOutcome
		OutcomeReason    problemOutcomeReason
		ExerciseEvidence []string
		Fixed            int
		Inconclusive     int
	}{
		Outcome:       problemOutcomeInconclusive,
		OutcomeReason: problemOutcomeReasonExerciseEvidenceMissing,
		ExerciseEvidence: []string{
			"normalized_response_body_match",
			"no_precondition_loss_detected",
		},
		Inconclusive: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport absent reproduction evidence = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsServerErrorProblemInconclusiveWhenResponseBodyUnavailable(
	t *testing.T,
) {
	tests := []struct {
		name             string
		baselineResponse *harResponse
		candidateBody    string
	}{
		{
			name:             "baseline response unavailable",
			baselineResponse: nil,
			candidateBody:    `{"error":"boom"}`,
		},
		{
			name: "baseline response body unavailable",
			baselineResponse: &harResponse{
				Status:  500,
				Content: harContent{},
			},
			candidateBody: `{"error":"boom"}`,
		},
		{
			name: "candidate response body unavailable",
			baselineResponse: &harResponse{
				Status:  500,
				Content: harContent{Text: `{"error":"boom"}`},
			},
			candidateBody: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interaction := 1
			document := newReport(reportInput{
				BaselineProblemEvidence: baselineProblemEvidence{
					Available: true,
					Problems: []baselineProblem{
						{
							CheckName:         "not_a_server_error",
							EvidenceSource:    evidenceSourceVCR,
							CaseID:            "case-42",
							CorrelationStatus: correlationStatusCorrelated,
							Interaction:       &interaction,
							Reproduction: problemReproduction{
								Method: "GET",
								URL:    "https://baseline.example.test/widgets/a8f31",
							},
						},
					},
				},
				Interactions: []reportInteraction{
					{
						Baseline: harEntry{
							Request: harRequest{
								Method: "GET",
								URL:    "https://baseline.example.test/widgets/a8f31",
							},
							Response: test.baselineResponse,
						},
						Replay: replayResult{
							Entry: harEntry{
								Response: &harResponse{
									Status:  200,
									Content: harContent{Text: test.candidateBody},
								},
							},
						},
					},
				},
			})

			got := struct {
				Outcome       problemOutcome
				OutcomeReason problemOutcomeReason
				Fixed         int
				Inconclusive  int
			}{
				Outcome:       document.Problems[0].Outcome,
				OutcomeReason: document.Problems[0].OutcomeReason,
				Fixed:         document.Summary.BaselineProblems.Fixed,
				Inconclusive:  document.Summary.BaselineProblems.Inconclusive,
			}
			want := struct {
				Outcome       problemOutcome
				OutcomeReason problemOutcomeReason
				Fixed         int
				Inconclusive  int
			}{
				Outcome:       problemOutcomeInconclusive,
				OutcomeReason: problemOutcomeReasonExerciseEvidenceMissing,
				Inconclusive:  1,
			}
			if got != want {
				t.Fatalf("newReport unavailable body outcome = %#v, want %#v", got, want)
			}
		})
	}
}

func TestNewReportKeepsPreconditionLossInconclusiveDespiteExerciseEvidence(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		PreconditionPolicy: PreconditionPolicy{
			MissingResourceStatuses: []int{404},
			Heuristics: []PreconditionHeuristic{
				NewPreconditionHeuristic(
					"generated-widget",
					"GET",
					`^/widgets/[0-9a-f]+$`,
				),
			},
		},
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
					Response: &harResponse{
						Status:  200,
						Content: harContent{Text: `{"error":"boom"}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  404,
							Content: harContent{Text: `{"error":"boom"}`},
						},
					},
				},
			},
		},
	})

	got := struct {
		Outcome                      problemOutcome
		OutcomeReason                problemOutcomeReason
		ExerciseEvidence             []string
		MatchedPreconditionHeuristic string
		Fixed                        int
		Inconclusive                 int
	}{
		Outcome:                      document.Problems[0].Outcome,
		OutcomeReason:                document.Problems[0].OutcomeReason,
		ExerciseEvidence:             document.Problems[0].ExerciseEvidence,
		MatchedPreconditionHeuristic: document.Problems[0].MatchedPreconditionHeuristic,
		Fixed:                        document.Summary.BaselineProblems.Fixed,
		Inconclusive:                 document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		Outcome                      problemOutcome
		OutcomeReason                problemOutcomeReason
		ExerciseEvidence             []string
		MatchedPreconditionHeuristic string
		Fixed                        int
		Inconclusive                 int
	}{
		Outcome:       problemOutcomeInconclusive,
		OutcomeReason: problemOutcomeReasonGeneratedResourcePreconditionLoss,
		ExerciseEvidence: []string{
			"operation_and_path_match",
			"normalized_response_body_match",
		},
		MatchedPreconditionHeuristic: "generated-widget",
		Inconclusive:                 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport precondition precedence = %#v, want %#v", got, want)
	}
}

func TestNewReportUsesNormalizationForServerErrorExerciseEvidence(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		PreconditionPolicy: PreconditionPolicy{
			Normalization: ResponseNormalizationConfig{
				BodyFields: []BodyFieldNormalizationRule{
					{Name: "generated-id", FieldName: "id"},
				},
			},
		},
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
					Response: &harResponse{
						Status:  500,
						Content: harContent{Text: `{"id":"baseline-1","name":"Ada"}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  200,
							Content: harContent{Text: `{"id":"candidate-9","name":"Ada"}`},
						},
					},
				},
			},
		},
	})

	got := struct {
		Outcome          problemOutcome
		ExerciseEvidence []string
		Fixed            int
	}{
		Outcome:          document.Problems[0].Outcome,
		ExerciseEvidence: document.Problems[0].ExerciseEvidence,
		Fixed:            document.Summary.BaselineProblems.Fixed,
	}
	want := struct {
		Outcome          problemOutcome
		ExerciseEvidence []string
		Fixed            int
	}{
		Outcome: problemOutcomeFixed,
		ExerciseEvidence: []string{
			"operation_and_path_match",
			"normalized_response_body_match",
			"no_precondition_loss_detected",
		},
		Fixed: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport normalization exercise evidence = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesMatchingPreconditionLossInconclusive(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		PreconditionPolicy: PreconditionPolicy{
			MissingResourceStatuses: []int{404, 410},
			Heuristics: []PreconditionHeuristic{
				NewPreconditionHeuristic(
					"generated-widget",
					"GET",
					`^/widgets/[0-9a-f]+$`,
				),
			},
		},
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL: "https://baseline.example.test/widgets/a8f31" +
							"?expand=owner",
					},
					Response: &harResponse{Status: 200},
				},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 404}},
				},
			},
		},
	})

	got := struct {
		Outcome                      problemOutcome
		OutcomeReason                string
		MatchedPreconditionHeuristic string
		Evaluable                    int
		Inconclusive                 int
	}{
		Outcome:                      document.Problems[0].Outcome,
		OutcomeReason:                string(document.Problems[0].OutcomeReason),
		MatchedPreconditionHeuristic: document.Problems[0].MatchedPreconditionHeuristic,
		Evaluable:                    document.Summary.BaselineProblems.Evaluable,
		Inconclusive:                 document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		Outcome                      problemOutcome
		OutcomeReason                string
		MatchedPreconditionHeuristic string
		Evaluable                    int
		Inconclusive                 int
	}{
		Outcome:                      problemOutcomeInconclusive,
		OutcomeReason:                "generated_resource_precondition_loss",
		MatchedPreconditionHeuristic: "generated-widget",
		Evaluable:                    1,
		Inconclusive:                 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport precondition loss classification = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesEveryProblemForMatchingPreconditionLoss(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		PreconditionPolicy: PreconditionPolicy{
			MissingResourceStatuses: []int{404, 410},
			Heuristics: []PreconditionHeuristic{
				NewPreconditionHeuristic(
					"generated-widget",
					"GET",
					`^/widgets/[0-9a-f]+$`,
				),
			},
		},
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-schema",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-server-error",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "GET",
						URL:    "https://baseline.example.test/widgets/a8f31",
					},
					Response: &harResponse{Status: 200},
				},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 404}},
				},
			},
		},
	})

	type problemClassification struct {
		CheckName                    string
		Outcome                      problemOutcome
		OutcomeReason                string
		MatchedPreconditionHeuristic string
	}
	got := struct {
		Problems     []problemClassification
		Evaluable    int
		Inconclusive int
		Fixed        int
		StillFailing int
		OutcomeTotal int
	}{
		Problems: []problemClassification{
			{
				CheckName:                    document.Problems[0].CheckName,
				Outcome:                      document.Problems[0].Outcome,
				OutcomeReason:                string(document.Problems[0].OutcomeReason),
				MatchedPreconditionHeuristic: document.Problems[0].MatchedPreconditionHeuristic,
			},
			{
				CheckName:                    document.Problems[1].CheckName,
				Outcome:                      document.Problems[1].Outcome,
				OutcomeReason:                string(document.Problems[1].OutcomeReason),
				MatchedPreconditionHeuristic: document.Problems[1].MatchedPreconditionHeuristic,
			},
		},
		Evaluable:    document.Summary.BaselineProblems.Evaluable,
		Inconclusive: document.Summary.BaselineProblems.Inconclusive,
		Fixed:        document.Summary.BaselineProblems.Fixed,
		StillFailing: document.Summary.BaselineProblems.StillFailing,
		OutcomeTotal: document.Summary.BaselineProblems.Fixed +
			document.Summary.BaselineProblems.StillFailing +
			document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		Problems     []problemClassification
		Evaluable    int
		Inconclusive int
		Fixed        int
		StillFailing int
		OutcomeTotal int
	}{
		Problems: []problemClassification{
			{
				CheckName:                    "response_schema_conformance",
				Outcome:                      problemOutcomeInconclusive,
				OutcomeReason:                "generated_resource_precondition_loss",
				MatchedPreconditionHeuristic: "generated-widget",
			},
			{
				CheckName:                    "not_a_server_error",
				Outcome:                      problemOutcomeInconclusive,
				OutcomeReason:                "generated_resource_precondition_loss",
				MatchedPreconditionHeuristic: "generated-widget",
			},
		},
		Evaluable:    2,
		Inconclusive: 2,
		Fixed:        0,
		StillFailing: 0,
		OutcomeTotal: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport shared precondition loss classifications = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesPreconditionPolicyBoundaries(t *testing.T) {
	generatedWidget := NewPreconditionHeuristic(
		"generated-widget",
		"GET",
		`^/widgets/[0-9a-f]+$`,
	)
	policy := func(statuses ...int) PreconditionPolicy {
		return PreconditionPolicy{
			MissingResourceStatuses: statuses,
			Heuristics:              []PreconditionHeuristic{generatedWidget},
		}
	}
	status := func(value int) *int {
		return &value
	}
	type classificationOutcome struct {
		Outcome                      problemOutcome
		OutcomeReason                string
		MatchedPreconditionHeuristic string
		Evaluable                    int
		StillFailing                 int
		Inconclusive                 int
	}
	tests := []struct {
		name            string
		policy          PreconditionPolicy
		checkName       string
		requestURL      string
		baselineStatus  *int
		candidateStatus int
		want            classificationOutcome
	}{
		{
			name:            "unmatched path",
			policy:          policy(404),
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/accounts/7",
			baselineStatus:  status(200),
			candidateStatus: 404,
			want: classificationOutcome{
				Outcome:       problemOutcomeInconclusive,
				OutcomeReason: "changed_outcome",
				Evaluable:     1,
				Inconclusive:  1,
			},
		},
		{
			name:            "absent baseline response",
			policy:          policy(404),
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			candidateStatus: 404,
			want: classificationOutcome{
				Outcome:       problemOutcomeInconclusive,
				OutcomeReason: "changed_outcome",
				Evaluable:     1,
				Inconclusive:  1,
			},
		},
		{
			name:            "baseline non-2xx",
			policy:          policy(404),
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			baselineStatus:  status(302),
			candidateStatus: 404,
			want: classificationOutcome{
				Outcome:       problemOutcomeInconclusive,
				OutcomeReason: "changed_outcome",
				Evaluable:     1,
				Inconclusive:  1,
			},
		},
		{
			name:            "configured 403",
			policy:          policy(403),
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			baselineStatus:  status(200),
			candidateStatus: 403,
			want: classificationOutcome{
				Outcome:                      problemOutcomeInconclusive,
				OutcomeReason:                "generated_resource_precondition_loss",
				MatchedPreconditionHeuristic: "generated-widget",
				Evaluable:                    1,
				Inconclusive:                 1,
			},
		},
		{
			name: "lowercase configured method",
			policy: PreconditionPolicy{
				MissingResourceStatuses: []int{404},
				Heuristics: []PreconditionHeuristic{
					NewPreconditionHeuristic(
						"generated-widget",
						"get",
						`^/widgets/[0-9a-f]+$`,
					),
				},
			},
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			baselineStatus:  status(200),
			candidateStatus: 404,
			want: classificationOutcome{
				Outcome:                      problemOutcomeInconclusive,
				OutcomeReason:                "generated_resource_precondition_loss",
				MatchedPreconditionHeuristic: "generated-widget",
				Evaluable:                    1,
				Inconclusive:                 1,
			},
		},
		{
			name: "first matching heuristic wins",
			policy: PreconditionPolicy{
				MissingResourceStatuses: []int{404},
				Heuristics: []PreconditionHeuristic{
					NewPreconditionHeuristic(
						"first-widget",
						"GET",
						`^/widgets/.*$`,
					),
					NewPreconditionHeuristic(
						"second-widget",
						"GET",
						`^/widgets/[0-9a-f]+$`,
					),
				},
			},
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			baselineStatus:  status(200),
			candidateStatus: 404,
			want: classificationOutcome{
				Outcome:                      problemOutcomeInconclusive,
				OutcomeReason:                "generated_resource_precondition_loss",
				MatchedPreconditionHeuristic: "first-widget",
				Evaluable:                    1,
				Inconclusive:                 1,
			},
		},
		{
			name:            "server-error checks still fail on candidate 5xx",
			policy:          policy(500),
			checkName:       "not_a_server_error",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			baselineStatus:  status(200),
			candidateStatus: 500,
			want: classificationOutcome{
				Outcome:      problemOutcomeStillFailing,
				Evaluable:    1,
				StillFailing: 1,
			},
		},
		{
			name:            "non-server-error checks do not match heuristics on candidate 5xx",
			policy:          policy(500),
			checkName:       "response_schema_conformance",
			requestURL:      "https://baseline.example.test/widgets/a8f31",
			baselineStatus:  status(200),
			candidateStatus: 500,
			want: classificationOutcome{
				Outcome:       problemOutcomeInconclusive,
				OutcomeReason: "changed_outcome",
				Evaluable:     1,
				Inconclusive:  1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interaction := 1
			var baselineResponse *harResponse
			if test.baselineStatus != nil {
				baselineResponse = &harResponse{Status: *test.baselineStatus}
			}
			document := newReport(reportInput{
				PreconditionPolicy: test.policy,
				BaselineProblemEvidence: baselineProblemEvidence{
					Available: true,
					Problems: []baselineProblem{
						{
							CheckName:         test.checkName,
							EvidenceSource:    evidenceSourceVCR,
							CaseID:            "case-42",
							CorrelationStatus: correlationStatusCorrelated,
							Interaction:       &interaction,
						},
					},
				},
				Interactions: []reportInteraction{
					{
						Baseline: harEntry{
							Request: harRequest{
								Method: "GET",
								URL:    test.requestURL,
							},
							Response: baselineResponse,
						},
						Replay: replayResult{
							Entry: harEntry{
								Response: &harResponse{Status: test.candidateStatus},
							},
						},
					},
				},
			})

			got := classificationOutcome{
				Outcome:                      document.Problems[0].Outcome,
				OutcomeReason:                string(document.Problems[0].OutcomeReason),
				MatchedPreconditionHeuristic: document.Problems[0].MatchedPreconditionHeuristic,
				Evaluable:                    document.Summary.BaselineProblems.Evaluable,
				StillFailing:                 document.Summary.BaselineProblems.StillFailing,
				Inconclusive:                 document.Summary.BaselineProblems.Inconclusive,
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("newReport precondition boundary = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNewReportClassifiesCandidateServerErrorWithoutBaselineProblemAsRegression(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 503}},
				},
			},
		},
	})

	got := struct {
		InteractionCount int
		Classification   interactionClassification
		RegressedTraffic int
	}{
		InteractionCount: len(document.Findings),
		Classification:   document.Findings[0].Classification,
		RegressedTraffic: document.Summary.Traffic.Regressed,
	}
	want := struct {
		InteractionCount int
		Classification   interactionClassification
		RegressedTraffic int
	}{
		InteractionCount: 1,
		Classification:   interactionClassificationRegressed,
		RegressedTraffic: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport regression classification = %#v, want %#v", got, want)
	}
}

func TestNewReportCountsAndOmitsHealthyUnchangedInteractionsFromFindings(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 200}},
				},
			},
		},
	})

	got := struct {
		Findings                int
		SuccessUnchangedTraffic int
	}{
		Findings:                len(document.Findings),
		SuccessUnchangedTraffic: document.Summary.Traffic.SuccessUnchanged,
	}
	want := struct {
		Findings                int
		SuccessUnchangedTraffic int
	}{
		Findings:                0,
		SuccessUnchangedTraffic: 1,
	}
	if got != want {
		t.Fatalf("newReport healthy unchanged traffic = %#v, want %#v", got, want)
	}
}

func TestNewReportSeparatesBaselineProblemAndTrafficTotals(t *testing.T) {
	interaction := 1
	nonServerErrorInteraction := 2
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-correlated",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-correlated-schema",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &nonServerErrorInteraction,
				},
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-uncorrelated",
					CorrelationStatus: correlationStatusUncorrelated,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 500}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 502}},
				},
			},
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 404}},
				},
			},
		},
	})

	got := struct {
		ProblemTotal      int
		EvaluableProblems int
		Uncorrelated      int
		OutcomeTotal      int
		TrafficTotal      int
		ChangedTraffic    int
	}{
		ProblemTotal:      document.Summary.BaselineProblems.Total,
		EvaluableProblems: document.Summary.BaselineProblems.Evaluable,
		Uncorrelated:      document.Summary.BaselineProblems.Uncorrelated,
		OutcomeTotal: document.Summary.BaselineProblems.Fixed +
			document.Summary.BaselineProblems.StillFailing +
			document.Summary.BaselineProblems.Inconclusive,
		TrafficTotal:   document.Summary.Traffic.Total,
		ChangedTraffic: document.Summary.Traffic.Changed,
	}
	want := struct {
		ProblemTotal      int
		EvaluableProblems int
		Uncorrelated      int
		OutcomeTotal      int
		TrafficTotal      int
		ChangedTraffic    int
	}{
		ProblemTotal:      3,
		EvaluableProblems: 2,
		Uncorrelated:      1,
		OutcomeTotal:      2,
		TrafficTotal:      2,
		ChangedTraffic:    2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport separated summary totals = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCandidateServerErrorRegressionWithDifferentBaselineCheck(
	t *testing.T,
) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "response_schema_conformance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 200}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 500}},
				},
			},
		},
	})

	got := struct {
		Classification interactionClassification
		Regressed      int
	}{
		Classification: document.Findings[0].Classification,
		Regressed:      document.Summary.Traffic.Regressed,
	}
	want := struct {
		Classification interactionClassification
		Regressed      int
	}{
		Classification: interactionClassificationRegressed,
		Regressed:      1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport non-corresponding server error regression = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesUnknownBaselineResponseTraffic(t *testing.T) {
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 200}},
				},
			},
			{
				Baseline: harEntry{},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 503}},
				},
			},
		},
	})

	got := struct {
		FirstClassification  interactionClassification
		SecondClassification interactionClassification
		Changed              int
		Regressed            int
	}{
		FirstClassification:  document.Findings[0].Classification,
		SecondClassification: document.Findings[1].Classification,
		Changed:              document.Summary.Traffic.Changed,
		Regressed:            document.Summary.Traffic.Regressed,
	}
	want := struct {
		FirstClassification  interactionClassification
		SecondClassification interactionClassification
		Changed              int
		Regressed            int
	}{
		FirstClassification:  interactionClassificationChanged,
		SecondClassification: interactionClassificationRegressed,
		Changed:              1,
		Regressed:            1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport unknown baseline traffic = %#v, want %#v", got, want)
	}
}

func TestNewReportKeepsUncategorizedProblemInconclusive(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "unsupported_method",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-unsupported",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{Response: &harResponse{Status: 501}},
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 405}},
				},
			},
		},
	})

	got := struct {
		CheckName     string
		CheckCategory checkCategory
		Outcome       problemOutcome
		OutcomeReason problemOutcomeReason
		Evaluable     int
		Inconclusive  int
		Fixed         int
		StillFailing  int
		OutcomeTotal  int
	}{
		CheckName:     document.Problems[0].CheckName,
		CheckCategory: document.Problems[0].CheckCategory,
		Outcome:       document.Problems[0].Outcome,
		OutcomeReason: document.Problems[0].OutcomeReason,
		Evaluable:     document.Summary.BaselineProblems.Evaluable,
		Inconclusive:  document.Summary.BaselineProblems.Inconclusive,
		Fixed:         document.Summary.BaselineProblems.Fixed,
		StillFailing:  document.Summary.BaselineProblems.StillFailing,
		OutcomeTotal: document.Summary.BaselineProblems.Fixed +
			document.Summary.BaselineProblems.StillFailing +
			document.Summary.BaselineProblems.Inconclusive,
	}
	want := struct {
		CheckName     string
		CheckCategory checkCategory
		Outcome       problemOutcome
		OutcomeReason problemOutcomeReason
		Evaluable     int
		Inconclusive  int
		Fixed         int
		StillFailing  int
		OutcomeTotal  int
	}{
		CheckName:     "unsupported_method",
		CheckCategory: checkCategoryUncategorized,
		Outcome:       problemOutcomeInconclusive,
		OutcomeReason: problemOutcomeReasonNoCategorizerForCheck,
		Evaluable:     1,
		Inconclusive:  1,
		OutcomeTotal:  1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport uncategorized problem = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesCheckSpecificFailuresIndependently(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "not_a_server_error",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-server",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
				},
				{
					CheckName:         "negative_data_rejection",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-negative",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
				},
				{
					CheckName:         "positive_data_acceptance",
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-positive",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
					Response: &harResponse{
						Status:  500,
						Content: harContent{Text: `{"error":"boom"}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  400,
							Content: harContent{Text: `{"error":"boom"}`},
						},
					},
				},
			},
		},
	})

	got := []problemOutcome{
		document.Problems[0].Outcome,
		document.Problems[1].Outcome,
		document.Problems[2].Outcome,
	}
	want := []problemOutcome{
		problemOutcomeFixed,
		problemOutcomeFixed,
		problemOutcomeStillFailing,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newReport independent check outcomes = %#v, want %#v", got, want)
	}
}

func TestNewReportClassifiesNegativeDataRejectionOutcomes(t *testing.T) {
	tests := []struct {
		name            string
		candidateStatus int
		wantOutcome     problemOutcome
		wantReason      problemOutcomeReason
		wantFixed       int
		wantStill       int
		wantInconcl     int
	}{
		{
			name:            "candidate accepts invalid data",
			candidateStatus: 201,
			wantOutcome:     problemOutcomeStillFailing,
			wantReason:      problemOutcomeReasonAcceptedInvalidData,
			wantStill:       1,
		},
		{
			name:            "candidate rejects invalid data",
			candidateStatus: 400,
			wantOutcome:     problemOutcomeFixed,
			wantReason:      problemOutcomeReasonValidationRejection,
			wantFixed:       1,
		},
		{
			name:            "candidate changes to redirect",
			candidateStatus: 302,
			wantOutcome:     problemOutcomeInconclusive,
			wantReason:      problemOutcomeReasonChangedOutcome,
			wantInconcl:     1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newSingleProblemReport("negative_data_rejection", test.candidateStatus)

			got := struct {
				Category  checkCategory
				Outcome   problemOutcome
				Reason    problemOutcomeReason
				Fixed     int
				Still     int
				Inconcl   int
				Evaluable int
			}{
				Category:  document.Problems[0].CheckCategory,
				Outcome:   document.Problems[0].Outcome,
				Reason:    document.Problems[0].OutcomeReason,
				Fixed:     document.Summary.BaselineProblems.Fixed,
				Still:     document.Summary.BaselineProblems.StillFailing,
				Inconcl:   document.Summary.BaselineProblems.Inconclusive,
				Evaluable: document.Summary.BaselineProblems.Evaluable,
			}
			want := struct {
				Category  checkCategory
				Outcome   problemOutcome
				Reason    problemOutcomeReason
				Fixed     int
				Still     int
				Inconcl   int
				Evaluable int
			}{
				Category:  checkCategoryNegativeDataRejection,
				Outcome:   test.wantOutcome,
				Reason:    test.wantReason,
				Fixed:     test.wantFixed,
				Still:     test.wantStill,
				Inconcl:   test.wantInconcl,
				Evaluable: 1,
			}
			if got != want {
				t.Fatalf("newReport negative-data outcome = %#v, want %#v", got, want)
			}
		})
	}
}

func TestNewReportClassifiesPositiveDataAcceptanceOutcomes(t *testing.T) {
	tests := []struct {
		name            string
		candidateStatus int
		wantOutcome     problemOutcome
		wantReason      problemOutcomeReason
		wantFixed       int
		wantStill       int
		wantInconcl     int
	}{
		{
			name:            "candidate accepts valid data",
			candidateStatus: 201,
			wantOutcome:     problemOutcomeFixed,
			wantReason:      problemOutcomeReasonAcceptedPositiveData,
			wantFixed:       1,
		},
		{
			name:            "candidate rejects valid data",
			candidateStatus: 400,
			wantOutcome:     problemOutcomeStillFailing,
			wantReason:      problemOutcomeReasonRepeatedRejection,
			wantStill:       1,
		},
		{
			name:            "candidate reports state conflict",
			candidateStatus: 409,
			wantOutcome:     problemOutcomeInconclusive,
			wantReason:      problemOutcomeReasonStateConflict,
			wantInconcl:     1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newSingleProblemReport("positive_data_acceptance", test.candidateStatus)

			got := struct {
				Category  checkCategory
				Outcome   problemOutcome
				Reason    problemOutcomeReason
				Fixed     int
				Still     int
				Inconcl   int
				Evaluable int
			}{
				Category:  document.Problems[0].CheckCategory,
				Outcome:   document.Problems[0].Outcome,
				Reason:    document.Problems[0].OutcomeReason,
				Fixed:     document.Summary.BaselineProblems.Fixed,
				Still:     document.Summary.BaselineProblems.StillFailing,
				Inconcl:   document.Summary.BaselineProblems.Inconclusive,
				Evaluable: document.Summary.BaselineProblems.Evaluable,
			}
			want := struct {
				Category  checkCategory
				Outcome   problemOutcome
				Reason    problemOutcomeReason
				Fixed     int
				Still     int
				Inconcl   int
				Evaluable int
			}{
				Category:  checkCategoryPositiveDataAcceptance,
				Outcome:   test.wantOutcome,
				Reason:    test.wantReason,
				Fixed:     test.wantFixed,
				Still:     test.wantStill,
				Inconcl:   test.wantInconcl,
				Evaluable: 1,
			}
			if got != want {
				t.Fatalf("newReport positive-data outcome = %#v, want %#v", got, want)
			}
		})
	}
}

func TestNewReportClassifiesResponseSchemaReplayEvidence(t *testing.T) {
	interaction := 1
	document := newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         "Response violates schema",
					EvidenceSource:    evidenceSourceJUnit,
					CaseID:            "case-schema",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
					Response: &harResponse{
						Status:  201,
						Content: harContent{Text: `{"unexpected":true}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  201,
							Content: harContent{Text: `{"unexpected":true}`},
						},
					},
				},
			},
		},
	})

	got := struct {
		Category checkCategory
		Outcome  problemOutcome
		Reason   problemOutcomeReason
		Still    int
	}{
		Category: document.Problems[0].CheckCategory,
		Outcome:  document.Problems[0].Outcome,
		Reason:   document.Problems[0].OutcomeReason,
		Still:    document.Summary.BaselineProblems.StillFailing,
	}
	want := struct {
		Category checkCategory
		Outcome  problemOutcome
		Reason   problemOutcomeReason
		Still    int
	}{
		Category: checkCategoryResponseSchemaConformance,
		Outcome:  problemOutcomeStillFailing,
		Reason:   problemOutcomeReasonRepeatedSchemaViolation,
		Still:    1,
	}
	if got != want {
		t.Fatalf("newReport response-schema replay evidence = %#v, want %#v", got, want)
	}
}

func newSingleProblemReport(checkName string, candidateStatus int) report {
	interaction := 1
	return newReport(reportInput{
		BaselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems: []baselineProblem{
				{
					CheckName:         checkName,
					EvidenceSource:    evidenceSourceVCR,
					CaseID:            "case-42",
					CorrelationStatus: correlationStatusCorrelated,
					Interaction:       &interaction,
					Reproduction: problemReproduction{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
				},
			},
		},
		Interactions: []reportInteraction{
			{
				Baseline: harEntry{
					Request: harRequest{
						Method: "POST",
						URL:    "https://baseline.example.test/widgets",
					},
					Response: &harResponse{
						Status:  201,
						Content: harContent{Text: `{"name":"Ada"}`},
					},
				},
				Replay: replayResult{
					Entry: harEntry{
						Response: &harResponse{
							Status:  candidateStatus,
							Content: harContent{Text: `{"name":"Ada"}`},
						},
					},
				},
			},
		},
	})
}
