package audit

import (
	"encoding/json"
	"fmt"
	"os"

	"stcompare/benchrecord"
)

// SchemaVersion is the version of the benchmark audit artifact.
const SchemaVersion = "1"

// Artifact is the durable local-model audit document.
type Artifact struct {
	SchemaVersion      string                        `json:"schema_version"`
	Run                Run                           `json:"run"`
	Capture            Capture                       `json:"capture"`
	Iterations         []Iteration                   `json:"iterations"`
	SharedContent      map[string]json.RawMessage    `json:"shared_content,omitempty"`
	Activity           benchrecord.ActivitySummary   `json:"activity"`
	Efficiency         benchrecord.EfficiencySummary `json:"efficiency"`
	FileModifications  []FileModification            `json:"file_modifications,omitempty"`
	FinalSource        FinalSource                   `json:"final_source,omitempty"`
	LifecycleChanges   []SourceChange                `json:"lifecycle_changes,omitempty"`
	ComparisonOutcomes []ComparisonOutcome           `json:"comparison_outcomes,omitempty"`
	EditSequences      []EditSequence                `json:"edit_sequences,omitempty"`
	Events             []Event                       `json:"events"`
}

// Run identifies the benchmark run associated with the audit.
type Run struct {
	ID            string `json:"id"`
	Candidate     string `json:"candidate"`
	Baseline      string `json:"baseline"`
	Agent         string `json:"agent"`
	Model         string `json:"model"`
	Effort        string `json:"effort"`
	Hardware      string `json:"hardware"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at,omitempty"`
	TerminalState string `json:"terminal_state,omitempty"`
}

// Capture describes whether the artifact contains a complete run capture.
type Capture struct {
	Enabled             bool   `json:"enabled"`
	Status              string `json:"status"`
	Complete            bool   `json:"complete"`
	Failure             string `json:"failure,omitempty"`
	FinishedAt          string `json:"finished_at,omitempty"`
	RecordingOverheadMS int64  `json:"recording_overhead_ms"`
}

// Iteration identifies an ordered benchmark iteration, its model turns, and
// its activity summary.
type Iteration struct {
	ID         string                        `json:"id"`
	Number     int                           `json:"number"`
	TurnIDs    []string                      `json:"turn_ids"`
	Activity   benchrecord.ActivitySummary   `json:"activity"`
	Efficiency benchrecord.EfficiencySummary `json:"efficiency"`
}

// ContentReference identifies shared message content used by one model input.
type ContentReference struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

// FileModification is one content-changing file operation. Before and After
// retain JSON strings, or null when the file did not exist at that point.
type FileModification struct {
	Sequence           int             `json:"sequence"`
	ID                 string          `json:"id"`
	Path               string          `json:"path"`
	Operation          string          `json:"operation"`
	ToolName           string          `json:"tool_name"`
	RunID              string          `json:"run_id"`
	ModelToolCallID    string          `json:"model_tool_call_id"`
	AdapterOperationID string          `json:"adapter_operation_id"`
	TurnID             string          `json:"turn_id"`
	IterationID        string          `json:"iteration_id"`
	Iteration          int             `json:"iteration"`
	Before             json.RawMessage `json:"before"`
	After              json.RawMessage `json:"after"`
	Diff               string          `json:"diff"`
	Created            bool            `json:"created"`
}

// Event is one chronological model-turn or activity record. Model-turn input
// is reconstructable from its shared-content references, and Returned retains
// its exact JSON value from the adapter boundary.
type Event struct {
	Sequence               int                     `json:"sequence"`
	Type                   string                  `json:"type"`
	RunID                  string                  `json:"run_id"`
	IterationID            string                  `json:"iteration_id"`
	Iteration              int                     `json:"iteration"`
	TurnID                 string                  `json:"turn_id"`
	Status                 string                  `json:"status"`
	StartedAt              string                  `json:"started_at"`
	EndedAt                string                  `json:"ended_at,omitempty"`
	DurationMS             int64                   `json:"duration_ms,omitempty"`
	RecordingOverheadMS    int64                   `json:"recording_overhead_ms,omitempty"`
	Sampling               map[string]any          `json:"sampling,omitempty"`
	Input                  json.RawMessage         `json:"input"`
	InputContentReferences []ContentReference      `json:"input_content_references,omitempty"`
	Returned               json.RawMessage         `json:"returned,omitempty"`
	ReturnedMessages       []json.RawMessage       `json:"returned_messages,omitempty"`
	Tokens                 *benchrecord.TokenUsage `json:"tokens,omitempty"`
	Error                  string                  `json:"error,omitempty"`
	ID                     string                  `json:"id,omitempty"`
	ToolCallID             string                  `json:"tool_call_id,omitempty"`
	ModelToolCallID        string                  `json:"model_tool_call_id,omitempty"`
	ToolName               string                  `json:"tool_name,omitempty"`
	Operation              string                  `json:"operation,omitempty"`
	Provenance             string                  `json:"provenance,omitempty"`
	Arguments              json.RawMessage         `json:"arguments,omitempty"`
	Request                json.RawMessage         `json:"request,omitempty"`
	Result                 json.RawMessage         `json:"result,omitempty"`
	EditAttempt            bool                    `json:"edit_attempt,omitempty"`
	FileModificationIDs    []string                `json:"file_modification_ids,omitempty"`
}

// Read loads and validates a versioned audit artifact.
func Read(path string) (Artifact, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("read audit artifact %q: %w", path, err)
	}
	var document Artifact
	if err := json.Unmarshal(contents, &document); err != nil {
		return Artifact{}, fmt.Errorf("parse audit artifact %q: %w", path, err)
	}
	if document.SchemaVersion != SchemaVersion {
		return Artifact{}, fmt.Errorf(
			"audit artifact %q has schema version %q, want %q",
			path,
			document.SchemaVersion,
			SchemaVersion,
		)
	}
	document.Activity = SummarizeActivity(document)
	document.Efficiency = SummarizeEfficiency(document)
	for index := range document.Iterations {
		document.Iterations[index].Activity = summarizeIterationActivity(document, document.Iterations[index].ID)
		document.Iterations[index].Efficiency = summarizeIterationEfficiency(document, document.Iterations[index].ID)
	}
	return document, nil
}
