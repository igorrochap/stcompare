package benchrecord

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRecordMarshalsBenchmarkRecordShape(t *testing.T) {
	record := Record{
		SchemaVersion: "1",
		Agent:         "agent",
		Model:         "model",
		Effort:        "high",
		Temperature:   0.65,
		Hardware:      "hardware",
		Prompt: PromptIdentity{
			ID:      "stbench-default",
			Version: "2026-01-01",
			Hash:    "template-hash",
		},
		PromptInstructions:   []string{"Task prompt stbench-default@2026-01-01"},
		RenderedPromptHashes: []string{"rendered-prompt-hash"},
		AgentResponses:       []string{"raw model response"},
		ProcessReuse:         true,
		Candidate:            "candidate",
		Baseline:             "baseline",
		StartedAt:            "2026-01-01T00:00:00Z",
		EndedAt:              "2026-01-01T00:01:00Z",
		Iterations:           2,
		TerminalState:        TerminalStateConverged,
		TimeMS: TimeBreakdown{
			Total:          60000,
			AgentFix:       20000,
			CandidateReset: 10000,
			Compare:        30000,
		},
		Tokens:                 &TokenUsage{Input: 10, Output: 20, Total: 30},
		UnknownTokenIterations: 1,
		Final: FinalSummary{
			Converged:    true,
			StillFailing: 0,
			Regressed:    0,
			Unverified: UnverifiedSummary{
				Inconclusive: 1,
				Uncorrelated: 2,
				Ambiguous:    3,
				Unevaluable:  4,
			},
		},
		RemainingActionable: []ActionableItem{{ID: "problem-1", Kind: "schema", Operation: "GET /widgets", Stuck: true}},
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	var decoded Record
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if !reflect.DeepEqual(decoded, record) {
		t.Fatalf("round-trip record = %#v, want %#v", decoded, record)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal record fields: %v", err)
	}
	if string(fields["schema_version"]) != `"1"` {
		t.Fatalf("schema_version = %s, want %q", fields["schema_version"], "1")
	}
	if string(fields["unknown_token_iterations"]) != `1` {
		t.Fatalf("unknown_token_iterations = %s, want 1", fields["unknown_token_iterations"])
	}
	if string(fields["temperature"]) != `0.65` {
		t.Fatalf("temperature = %s, want 0.65", fields["temperature"])
	}
}

func TestRecordTokensRoundTripNullDistinctFromPopulated(t *testing.T) {
	for _, test := range []struct {
		name   string
		tokens *TokenUsage
		wanted string
	}{
		{name: "unknown", tokens: nil, wanted: "null"},
		{name: "known", tokens: &TokenUsage{}, wanted: `{"input":0,"output":0,"total":0}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(Record{Tokens: test.tokens})
			if err != nil {
				t.Fatalf("marshal record: %v", err)
			}

			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("unmarshal record fields: %v", err)
			}
			if string(fields["tokens"]) != test.wanted {
				t.Fatalf("tokens = %s, want %s", fields["tokens"], test.wanted)
			}

			var decoded Record
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if !reflect.DeepEqual(decoded.Tokens, test.tokens) {
				t.Fatalf("round-trip tokens = %#v, want %#v", decoded.Tokens, test.tokens)
			}
		})
	}
}

func TestTerminalStatesMarshalAsFixedValues(t *testing.T) {
	for _, state := range []TerminalState{
		TerminalStateConverged,
		TerminalStateStalled,
		TerminalStateMaxIterations,
		TerminalStateToolError,
		TerminalStateAdapterError,
		TerminalStateLifecycleError,
	} {
		data, err := json.Marshal(Record{TerminalState: state})
		if err != nil {
			t.Fatalf("marshal %q: %v", state, err)
		}

		var decoded Record
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %q: %v", state, err)
		}
		if decoded.TerminalState != state {
			t.Fatalf("terminal_state = %q, want %q", decoded.TerminalState, state)
		}
	}
}
