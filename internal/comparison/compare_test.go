package comparison

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewReportInteractionsRejectsMismatchedInputs(t *testing.T) {
	_, err := newReportInteractions(
		[]harEntry{{}},
		[]replayResult{{}, {}},
	)
	if err == nil {
		t.Fatal("newReportInteractions accepted mismatched inputs")
	}

	want := "pair replay results with baseline entries: got 2 replay results for 1 baseline entries"
	if err.Error() != want {
		t.Fatalf("newReportInteractions error = %q, want %q", err.Error(), want)
	}
}

func TestPrepareComparisonStoresCorrelatedVCRProblemEvidence(t *testing.T) {
	tempDir := t.TempDir()
	harPath := filepath.Join(tempDir, "campaign.har.json")
	harContents := []byte(`
{
  "log": {
    "entries": [
      {
        "request": {
          "method": "POST",
          "url": "https://baseline.example.test/widgets",
          "headers": [
            {
              "name": "X-Schemathesis-TestCaseId",
              "value": "case-42"
            }
          ],
          "postData": {
            "text": "{\"name\":\"Ada\"}"
          }
        }
      }
    ]
  }
}`)
	if err := os.WriteFile(harPath, harContents, 0o644); err != nil {
		t.Fatalf("write HAR fixture: %v", err)
	}

	vcrPath := filepath.Join(tempDir, "campaign.vcr.yaml")
	vcrContents := []byte(`
http_interactions:
  - id: case-42
    checks:
      - name: status_code_conformance
        status: FAILURE
        message: "Received an undocumented status code: 418"
    request:
      uri: "https://baseline.example.test/widgets"
      method: POST
      headers:
        Content-Type:
          - application/json
      body:
        base64_string: eyJuYW1lIjoiQWRhIn0=
`)
	if err := os.WriteFile(vcrPath, vcrContents, 0o644); err != nil {
		t.Fatalf("write VCR fixture: %v", err)
	}

	prepared, err := prepareComparison(Input{
		BaselineHARPath:   harPath,
		BaselineVCRPath:   vcrPath,
		BaselineJUnitPath: filepath.Join(tempDir, "missing-junit.xml"),
		CandidateBaseURL:  "https://candidate.example.test",
	})
	if err != nil {
		t.Fatalf("prepareComparison returned error: %v", err)
	}

	interaction := 1
	want := baselineProblemEvidence{
		Available:  true,
		SourcePath: vcrPath,
		Problems: []baselineProblem{
			{
				CheckName:         "status_code_conformance",
				Message:           "Received an undocumented status code: 418",
				EvidenceSource:    "vcr",
				CaseID:            "case-42",
				CorrelationStatus: correlationStatusCorrelated,
				Reproduction: problemReproduction{
					Method: "POST",
					URL:    "https://baseline.example.test/widgets",
					Headers: []harHeader{
						{Name: "Content-Type", Value: "application/json"},
					},
					Body: `{"name":"Ada"}`,
				},
				Interaction: &interaction,
			},
		},
	}
	if !reflect.DeepEqual(prepared.baselineProblemEvidence, want) {
		t.Fatalf(
			"prepareComparison problem evidence = %#v, want %#v",
			prepared.baselineProblemEvidence,
			want,
		)
	}
}

func TestPrepareComparisonCorrelatesRealJUnitProblemThroughRealHAR(t *testing.T) {
	prepared, err := prepareComparison(Input{
		BaselineHARPath:   filepath.Join("testdata", "schemathesis-matched-real.har.json"),
		BaselineVCRPath:   filepath.Join(t.TempDir(), "missing.vcr.yaml"),
		BaselineJUnitPath: filepath.Join("testdata", "schemathesis-matched-real.junit.xml"),
		CandidateBaseURL:  "https://candidate.example.test",
	})
	if err != nil {
		t.Fatalf("prepareComparison returned error: %v", err)
	}

	got := struct {
		CheckName         string
		CaseID            string
		CorrelationStatus correlationStatus
		Interaction       *int
		Outcome           problemOutcome
	}{
		CheckName:         prepared.baselineProblemEvidence.Problems[0].CheckName,
		CaseID:            prepared.baselineProblemEvidence.Problems[0].CaseID,
		CorrelationStatus: prepared.baselineProblemEvidence.Problems[0].CorrelationStatus,
		Interaction:       prepared.baselineProblemEvidence.Problems[0].Interaction,
	}
	document := newReport(reportInput{
		BaselineProblemEvidence: prepared.baselineProblemEvidence,
		Interactions: []reportInteraction{
			{
				Baseline: prepared.baselineEntries[0],
				Replay: replayResult{
					Entry: harEntry{Response: &harResponse{Status: 502}},
				},
			},
		},
	})
	got.Outcome = document.Problems[0].Outcome
	interaction := 1
	want := struct {
		CheckName         string
		CaseID            string
		CorrelationStatus correlationStatus
		Interaction       *int
		Outcome           problemOutcome
	}{
		CheckName:         "Server error",
		CaseID:            "0DNKjC",
		CorrelationStatus: correlationStatusCorrelated,
		Interaction:       &interaction,
		Outcome:           problemOutcomeStillFailing,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("real JUnit/HAR correlation = %#v, want %#v", got, want)
	}
}

func TestPersistComparisonArtifactsWritesCorrelatedProblemEvidence(t *testing.T) {
	interaction := 1
	problem := baselineProblem{
		CheckName:         "status_code_conformance",
		Message:           "Received an undocumented status code: 418",
		EvidenceSource:    "vcr",
		CaseID:            "case-42",
		CorrelationStatus: correlationStatusCorrelated,
		Reproduction: problemReproduction{
			Method: "POST",
			URL:    "https://baseline.example.test/widgets",
			Body:   `{"name":"Ada"}`,
		},
		Interaction: &interaction,
	}
	prepared := preparedComparison{
		baselineEntries: []harEntry{
			{
				Request: harRequest{
					Method: "POST",
					URL:    "https://baseline.example.test/widgets",
				},
				Response: &harResponse{Status: 418},
			},
		},
		baselineProblemEvidence: baselineProblemEvidence{
			Available: true,
			Problems:  []baselineProblem{problem},
		},
	}
	replayResults := []replayResult{
		{
			Entry: harEntry{
				Response: &harResponse{Status: 200},
			},
			TargetURL: "https://candidate.example.test/widgets",
			LatencyMS: 10,
		},
	}

	result, err := persistComparisonArtifacts(
		Input{
			BaselineCampaign:  "baseline",
			CandidateCampaign: "candidate",
			CandidateBaseURL:  "https://candidate.example.test",
			OutputDir:         t.TempDir(),
		},
		prepared,
		replayResults,
	)
	if err != nil {
		t.Fatalf("persistComparisonArtifacts returned error: %v", err)
	}

	contents, err := os.ReadFile(result.JSONReportPath)
	if err != nil {
		t.Fatalf("read comparison JSON report: %v", err)
	}
	type problemReportState struct {
		Available bool              `json:"baseline_problems_available"`
		Problems  []baselineProblem `json:"problems"`
	}
	var got problemReportState
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode comparison JSON report: %v", err)
	}

	want := problemReportState{
		Available: true,
		Problems: []baselineProblem{
			func() baselineProblem {
				problem.CheckCategory = checkCategoryUncategorized
				problem.Outcome = problemOutcomeInconclusive
				problem.OutcomeReason = problemOutcomeReasonNoCategorizerForCheck
				return problem
			}(),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("comparison JSON problem state = %#v, want %#v", got, want)
	}
}

func TestPrepareComparisonSelectsAndCorrelatesFallbackProblemEvidence(t *testing.T) {
	interaction := 1
	tests := []struct {
		name   string
		caseID string
		ndjson string
		junit  string
		want   baselineProblemEvidence
	}{
		{
			name:   "NDJSON fallback",
			caseID: "case-ndjson",
			ndjson: `{"ScenarioFinished":{"recorder":{"checks":{"case-ndjson":[` +
				`{"name":"not_a_server_error","status":"failure",` +
				`"failure_info":{"failure":{"title":"Server error","message":"Received 500"}}}` +
				`]},"interactions":{"case-ndjson":{"request":{` +
				`"method":"GET","uri":"https://baseline.example.test/probe","headers":{}` +
				`}}}}}}` + "\n",
			want: baselineProblemEvidence{
				Available: true,
				Problems: []baselineProblem{
					{
						CheckName:         "not_a_server_error",
						Message:           "Received 500",
						EvidenceSource:    "ndjson",
						CaseID:            "case-ndjson",
						CorrelationStatus: correlationStatusCorrelated,
						Reproduction: problemReproduction{
							Method: "GET",
							URL:    "https://baseline.example.test/probe",
						},
						Interaction: &interaction,
					},
				},
			},
		},
		{
			name:   "structured JUnit fallback",
			caseID: "case-junit",
			junit: `
<testsuites>
  <testsuite>
    <testcase>
      <failure><![CDATA[
1. Test Case ID: case-junit

- API accepted schema-violating request

Server accepted invalid input.

Reproduce with:

curl https://baseline.example.test/probe
]]></failure>
    </testcase>
  </testsuite>
</testsuites>`,
			want: baselineProblemEvidence{
				Available: true,
				Problems: []baselineProblem{
					{
						CheckName:         "API accepted schema-violating request",
						Message:           "Server accepted invalid input.",
						EvidenceSource:    "junit",
						CaseID:            "case-junit",
						CorrelationStatus: correlationStatusCorrelated,
						Reproduction: problemReproduction{
							Command: "curl https://baseline.example.test/probe",
						},
						Interaction: &interaction,
					},
				},
			},
		},
		{
			name:   "legacy JUnit remains unavailable",
			caseID: "case-legacy",
			junit: `
<testsuites>
  <testsuite>
    <testcase>
      <failure message="legacy failure">plain legacy failure text</failure>
    </testcase>
  </testsuite>
</testsuites>`,
		},
	}

	got := make([]baselineProblemEvidence, 0, len(tests))
	want := make([]baselineProblemEvidence, 0, len(tests))
	for _, test := range tests {
		tempDir := t.TempDir()
		harPath := filepath.Join(tempDir, "campaign.har.json")
		harContents := []byte(fmt.Sprintf(`
{
  "log": {
    "entries": [
      {
        "request": {
          "method": "GET",
          "url": "https://baseline.example.test/probe",
          "headers": [
            {
              "name": "X-Schemathesis-TestCaseId",
              "value": %q
            }
          ],
          "postData": {}
        }
      }
    ]
  }
}`, test.caseID))
		if err := os.WriteFile(harPath, harContents, 0o644); err != nil {
			t.Fatalf("%s: write HAR fixture: %v", test.name, err)
		}

		ndjsonPath := filepath.Join(tempDir, "campaign.ndjson")
		if test.ndjson != "" {
			if err := os.WriteFile(ndjsonPath, []byte(test.ndjson), 0o644); err != nil {
				t.Fatalf("%s: write NDJSON fixture: %v", test.name, err)
			}
		}
		junitPath := filepath.Join(tempDir, "junit.xml")
		if test.junit != "" {
			if err := os.WriteFile(junitPath, []byte(test.junit), 0o644); err != nil {
				t.Fatalf("%s: write JUnit fixture: %v", test.name, err)
			}
		}
		if test.want.Available {
			if test.ndjson != "" {
				test.want.SourcePath = ndjsonPath
			} else {
				test.want.SourcePath = junitPath
			}
		}

		prepared, err := prepareComparison(Input{
			BaselineHARPath:    harPath,
			BaselineVCRPath:    filepath.Join(tempDir, "missing.vcr.yaml"),
			BaselineNDJSONPath: ndjsonPath,
			BaselineJUnitPath:  junitPath,
			CandidateBaseURL:   "https://candidate.example.test",
		})
		if err != nil {
			t.Fatalf("%s: prepareComparison returned error: %v", test.name, err)
		}
		got = append(got, prepared.baselineProblemEvidence)
		want = append(want, test.want)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prepareComparison fallback evidence = %#v, want %#v", got, want)
	}
}

func TestPrepareComparisonFallsThroughWhenHigherPrecedenceEvidenceIsIncomplete(t *testing.T) {
	tempDir := t.TempDir()
	harPath := filepath.Join(tempDir, "campaign.har.json")
	harContents := []byte(`
{
  "log": {
    "entries": [
      {
        "request": {
          "method": "GET",
          "url": "https://baseline.example.test/probe",
          "headers": [
            {
              "name": "X-Schemathesis-TestCaseId",
              "value": "case-ndjson"
            }
          ]
        }
      }
    ]
  }
}`)
	if err := os.WriteFile(harPath, harContents, 0o644); err != nil {
		t.Fatalf("write HAR fixture: %v", err)
	}

	vcrPath := filepath.Join(tempDir, "campaign.vcr.yaml")
	vcrContents := []byte(`
http_interactions:
  - id: case-vcr
    checks:
      - name: status_code_conformance
        status: BROKEN
        message: "Received an undocumented status code: 418"
    request:
      uri: "https://baseline.example.test/widgets"
      method: POST
      body:
        string: '{"name":"Ada"}'
`)
	if err := os.WriteFile(vcrPath, vcrContents, 0o644); err != nil {
		t.Fatalf("write VCR fixture: %v", err)
	}

	ndjsonPath := filepath.Join(tempDir, "campaign.ndjson")
	ndjsonContents := []byte(
		`{"ScenarioFinished":{"recorder":{"checks":{"case-ndjson":[` +
			`{"name":"not_a_server_error","status":"failure",` +
			`"failure_info":{"failure":{"title":"Server error","message":"Received 500"}}}` +
			`]},"interactions":{"case-ndjson":{"request":{` +
			`"method":"GET","uri":"https://baseline.example.test/probe","headers":{}` +
			`}}}}}}` + "\n",
	)
	if err := os.WriteFile(ndjsonPath, ndjsonContents, 0o644); err != nil {
		t.Fatalf("write NDJSON fixture: %v", err)
	}

	prepared, err := prepareComparison(Input{
		BaselineHARPath:    harPath,
		BaselineVCRPath:    vcrPath,
		BaselineNDJSONPath: ndjsonPath,
		BaselineJUnitPath:  filepath.Join(tempDir, "missing-junit.xml"),
		CandidateBaseURL:   "https://candidate.example.test",
	})
	if err != nil {
		t.Fatalf("prepareComparison returned error: %v", err)
	}

	interaction := 1
	want := baselineProblemEvidence{
		Available:  true,
		SourcePath: ndjsonPath,
		Problems: []baselineProblem{
			{
				CheckName:         "not_a_server_error",
				Message:           "Received 500",
				EvidenceSource:    evidenceSourceNDJSON,
				CaseID:            "case-ndjson",
				CorrelationStatus: correlationStatusCorrelated,
				Reproduction: problemReproduction{
					Method: "GET",
					URL:    "https://baseline.example.test/probe",
				},
				Interaction: &interaction,
			},
		},
	}
	if !reflect.DeepEqual(prepared.baselineProblemEvidence, want) {
		t.Fatalf(
			"prepareComparison incomplete-fallback evidence = %#v, want %#v",
			prepared.baselineProblemEvidence,
			want,
		)
	}
}

func TestPrepareComparisonRequiresCompleteJUnitProblemExtraction(t *testing.T) {
	const groupedFailure = `
<failure><![CDATA[
1. Test Case ID: case-1

- First failed check

First diagnostic.

- Second failed check

Second diagnostic.

Reproduce with:

curl https://baseline.example.test/one

2. Test Case ID: case-2

- Third failed check

Third diagnostic.

Reproduce with:

curl https://baseline.example.test/two
]]></failure>`
	tests := []struct {
		name              string
		additionalFailure string
		available         bool
	}{
		{
			name:      "complete grouped JUnit",
			available: true,
		},
		{
			name:              "mixed incomplete JUnit",
			additionalFailure: `<failure message="legacy">plain legacy failure</failure>`,
		},
	}

	type outcome struct {
		Name     string
		Evidence baselineProblemEvidence
	}
	got := make([]outcome, 0, len(tests))
	want := make([]outcome, 0, len(tests))
	for _, test := range tests {
		tempDir := t.TempDir()
		harPath := filepath.Join(tempDir, "campaign.har.json")
		harContents := []byte(`
{
  "log": {
    "entries": [
      {
        "request": {
          "method": "GET",
          "url": "https://baseline.example.test/probe",
          "headers": [
            {
              "name": "X-Schemathesis-TestCaseId",
              "value": "case-1"
            }
          ],
          "postData": {}
        }
      },
      {
        "request": {
          "method": "GET",
          "url": "https://baseline.example.test/probe",
          "headers": [
            {
              "name": "X-Schemathesis-TestCaseId",
              "value": "case-2"
            }
          ],
          "postData": {}
        }
      }
    ]
  }
}`)
		if err := os.WriteFile(harPath, harContents, 0o644); err != nil {
			t.Fatalf("%s: write HAR fixture: %v", test.name, err)
		}

		junitPath := filepath.Join(tempDir, "junit.xml")
		junitContents := []byte(
			"<testsuites><testsuite><testcase>" +
				groupedFailure +
				test.additionalFailure +
				"</testcase></testsuite></testsuites>",
		)
		if err := os.WriteFile(junitPath, junitContents, 0o644); err != nil {
			t.Fatalf("%s: write JUnit fixture: %v", test.name, err)
		}

		prepared, err := prepareComparison(Input{
			BaselineHARPath:    harPath,
			BaselineVCRPath:    filepath.Join(tempDir, "missing.vcr.yaml"),
			BaselineNDJSONPath: filepath.Join(tempDir, "missing.ndjson"),
			BaselineJUnitPath:  junitPath,
			CandidateBaseURL:   "https://candidate.example.test",
		})
		if err != nil {
			t.Fatalf("%s: prepareComparison returned error: %v", test.name, err)
		}
		got = append(got, outcome{Name: test.name, Evidence: prepared.baselineProblemEvidence})

		expected := baselineProblemEvidence{}
		if test.available {
			interactionOne := 1
			interactionTwo := 2
			expected = baselineProblemEvidence{
				Available:  true,
				SourcePath: junitPath,
				Problems: []baselineProblem{
					{
						CheckName:         "First failed check",
						Message:           "First diagnostic.",
						EvidenceSource:    "junit",
						CaseID:            "case-1",
						CorrelationStatus: correlationStatusCorrelated,
						Reproduction: problemReproduction{
							Command: "curl https://baseline.example.test/one",
						},
						Interaction: &interactionOne,
					},
					{
						CheckName:         "Second failed check",
						Message:           "Second diagnostic.",
						EvidenceSource:    "junit",
						CaseID:            "case-1",
						CorrelationStatus: correlationStatusCorrelated,
						Reproduction: problemReproduction{
							Command: "curl https://baseline.example.test/one",
						},
						Interaction: &interactionOne,
					},
					{
						CheckName:         "Third failed check",
						Message:           "Third diagnostic.",
						EvidenceSource:    "junit",
						CaseID:            "case-2",
						CorrelationStatus: correlationStatusCorrelated,
						Reproduction: problemReproduction{
							Command: "curl https://baseline.example.test/two",
						},
						Interaction: &interactionTwo,
					},
				},
			}
		}
		want = append(want, outcome{Name: test.name, Evidence: expected})
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prepareComparison JUnit completeness outcomes = %#v, want %#v", got, want)
	}
}
