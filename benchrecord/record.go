// Package benchrecord defines the versioned JSON contract for benchmark runs.
package benchrecord

// SchemaVersion is the current benchmark-record schema version.
const SchemaVersion = "1"

// TerminalState describes why a benchmark run ended.
type TerminalState string

const (
	// TerminalStateConverged indicates that the candidate converged.
	TerminalStateConverged TerminalState = "converged"
	// TerminalStateStalled indicates that the actionable work stopped making progress.
	TerminalStateStalled TerminalState = "stalled"
	// TerminalStateMaxIterations indicates that the iteration limit was reached.
	TerminalStateMaxIterations TerminalState = "max_iterations"
	// TerminalStateToolError indicates that the comparison tool failed.
	TerminalStateToolError TerminalState = "tool_error"
	// TerminalStateAdapterError indicates that the agent adapter failed.
	TerminalStateAdapterError TerminalState = "adapter_error"
	// TerminalStateLifecycleError indicates that candidate lifecycle management failed.
	TerminalStateLifecycleError TerminalState = "lifecycle_error"
)

// Record is the benchmark result for one agent, candidate, and run.
type Record struct {
	SchemaVersion       string           `json:"schema_version"`
	Agent               string           `json:"agent"`
	Model               string           `json:"model"`
	Hardware            string           `json:"hardware"`
	Prompt              PromptIdentity   `json:"prompt"`
	Candidate           string           `json:"candidate"`
	Baseline            string           `json:"baseline"`
	StartedAt           string           `json:"started_at"`
	EndedAt             string           `json:"ended_at"`
	Iterations          int              `json:"iterations"`
	TerminalState       TerminalState    `json:"terminal_state"`
	TimeMS              TimeBreakdown    `json:"time_ms"`
	Tokens              *TokenUsage      `json:"tokens"`
	Final               FinalSummary     `json:"final"`
	RemainingActionable []ActionableItem `json:"remaining_actionable"`
}

// PromptIdentity identifies the fixed task prompt used by a run.
type PromptIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// TimeBreakdown contains wall-clock duration measurements in milliseconds.
type TimeBreakdown struct {
	Total          int64 `json:"total"`
	AgentFix       int64 `json:"agent_fix"`
	CandidateReset int64 `json:"candidate_reset"`
	Compare        int64 `json:"compare"`
}

// TokenUsage contains token counts reported by an agent.
type TokenUsage struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
	Total  int64 `json:"total"`
}

// FinalSummary contains the convergence state at the end of a run.
type FinalSummary struct {
	Converged    bool              `json:"converged"`
	StillFailing int               `json:"still_failing"`
	Regressed    int               `json:"regressed"`
	Unverified   UnverifiedSummary `json:"unverified"`
}

// UnverifiedSummary contains residual problems without a verified outcome.
type UnverifiedSummary struct {
	Inconclusive int `json:"inconclusive"`
	Uncorrelated int `json:"uncorrelated"`
	Ambiguous    int `json:"ambiguous"`
	Unevaluable  int `json:"unevaluable"`
}

// ActionableItem identifies work remaining at the end of a run.
type ActionableItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	Stuck     bool   `json:"stuck"`
}
