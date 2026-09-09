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
	// TerminalStateAuditError indicates that required audit evidence could not be saved.
	TerminalStateAuditError TerminalState = "audit_error"
)

// LifecyclePhase identifies the phase that failed while preparing a run.
type LifecyclePhase string

const (
	// LifecyclePhaseBaselinePrecondition identifies a missing baseline campaign.
	LifecyclePhaseBaselinePrecondition LifecyclePhase = "baseline_precondition"
	// LifecyclePhaseStop identifies the candidate stop phase.
	LifecyclePhaseStop LifecyclePhase = "stop"
	// LifecyclePhaseReset identifies the candidate reset phase.
	LifecyclePhaseReset LifecyclePhase = "reset"
	// LifecyclePhaseBuild identifies the candidate build phase.
	LifecyclePhaseBuild LifecyclePhase = "build"
	// LifecyclePhaseStart identifies the candidate start phase.
	LifecyclePhaseStart LifecyclePhase = "start"
	// LifecyclePhaseWaitHealthy identifies the candidate health-check phase.
	LifecyclePhaseWaitHealthy LifecyclePhase = "wait_healthy"
)

// Record is the benchmark result for one agent, candidate, and run.
type Record struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Agent         string `json:"agent"`
	Model         string `json:"model"`
	Effort        string `json:"effort"`
	// Temperature is the effective sampling temperature used by the adapter.
	Temperature          float64        `json:"temperature"`
	Hardware             string         `json:"hardware"`
	Prompt               PromptIdentity `json:"prompt"`
	PromptInstructions   []string       `json:"prompt_instructions"`
	RenderedPromptHashes []string       `json:"rendered_prompt_hashes"`
	AgentResponses       []string       `json:"agent_responses"`
	// ProcessReuse reports whether the adapter negotiated a reusable process.
	ProcessReuse   bool           `json:"process_reuse"`
	Candidate      string         `json:"candidate"`
	Baseline       string         `json:"baseline"`
	StartedAt      string         `json:"started_at"`
	EndedAt        string         `json:"ended_at"`
	Iterations     int            `json:"iterations"`
	TerminalState  TerminalState  `json:"terminal_state"`
	LifecyclePhase LifecyclePhase `json:"lifecycle_phase,omitempty"`
	TimeMS         TimeBreakdown  `json:"time_ms"`
	Tokens         *TokenUsage    `json:"tokens"`
	// Efficiency contains the audited model-turn breakdown when audit evidence
	// is available. Legacy and unsupported-adapter records leave it not_reported.
	Efficiency EfficiencySummary `json:"efficiency"`
	// IterationEfficiency contains one aggregate per audited iteration, in
	// benchmark iteration order.
	IterationEfficiency []EfficiencySummary `json:"iteration_efficiency"`
	Audit               AuditReference      `json:"audit"`
	// UnknownTokenIterations counts fix iterations without reported token usage.
	UnknownTokenIterations int              `json:"unknown_token_iterations"`
	Final                  FinalSummary     `json:"final"`
	RemainingActionable    []ActionableItem `json:"remaining_actionable"`
}

// AuditStatus describes the availability of model-turn audit evidence.
type AuditStatus string

const (
	// AuditStatusComplete identifies a run whose captured evidence is complete.
	AuditStatusComplete AuditStatus = "complete"
	// AuditStatusPartial identifies evidence retained from an unfinished run or
	// an incomplete capture.
	AuditStatusPartial AuditStatus = "partial"
	// AuditStatusNotReported identifies legacy records and unsupported adapters.
	AuditStatusNotReported AuditStatus = "not_reported"
)

// AuditReference links a benchmark record to its local model-turn evidence.
// Paths are relative to the benchmark record's directory.
type AuditReference struct {
	Status   AuditStatus      `json:"status"`
	RunID    string           `json:"run_id,omitempty"`
	Artifact string           `json:"artifact,omitempty"`
	Report   string           `json:"report,omitempty"`
	Error    string           `json:"error,omitempty"`
	Activity *ActivitySummary `json:"activity,omitempty"`
}

// ActivityStatus describes the availability of activity evidence.
type ActivityStatus string

const (
	// ActivityStatusComplete identifies complete activity evidence.
	ActivityStatusComplete ActivityStatus = "complete"
	// ActivityStatusPartial identifies activity evidence from an incomplete capture.
	ActivityStatusPartial ActivityStatus = "partial"
	// ActivityStatusNotReported identifies activity that was not captured.
	ActivityStatusNotReported ActivityStatus = "not_reported"
)

// ActivitySummary contains counts and execution time for one audit scope.
type ActivitySummary struct {
	Status            ActivityStatus `json:"status"`
	ModelToolCalls    ActivityCounts `json:"model_tool_calls"`
	AdapterOperations ActivityCounts `json:"adapter_operations"`
	EditAttempts      int            `json:"edit_attempts"`
	FileModifications int            `json:"file_modifications"`
}

// ActivityCounts contains one activity category's measurable outcomes.
type ActivityCounts struct {
	Count      int   `json:"count"`
	Completed  int   `json:"completed"`
	Failed     int   `json:"failed"`
	Incomplete int   `json:"incomplete"`
	DurationMS int64 `json:"duration_ms"`
}

// TokenStatus describes how much server-reported token usage is available.
type TokenStatus string

const (
	// TokenStatusComplete means every audited model turn reported usage.
	TokenStatusComplete TokenStatus = "complete"
	// TokenStatusPartial means a known subtotal exists but one or more turns
	// did not report usage.
	TokenStatusPartial TokenStatus = "partial"
	// TokenStatusUnknown means model turns were captured but none reported usage.
	TokenStatusUnknown TokenStatus = "unknown"
	// TokenStatusNotReported means no model-turn usage evidence exists.
	TokenStatusNotReported TokenStatus = "not_reported"
)

// EfficiencyStatus describes whether the model-turn evidence is complete.
type EfficiencyStatus string

const (
	// EfficiencyStatusComplete means all captured model turns have terminal
	// outcomes and their timing evidence is complete.
	EfficiencyStatusComplete EfficiencyStatus = "complete"
	// EfficiencyStatusPartial means the run or capture ended with incomplete
	// model-turn evidence.
	EfficiencyStatusPartial EfficiencyStatus = "partial"
	// EfficiencyStatusNotReported means no model-turn audit was captured.
	EfficiencyStatusNotReported EfficiencyStatus = "not_reported"
)

// EfficiencySummary contains model-turn cost and timing for one audit scope.
// Tokens is the known server-reported subtotal; it is nil when no turn
// reported usage. It never includes estimates from model text.
type EfficiencySummary struct {
	Status                 EfficiencyStatus `json:"status"`
	Turns                  int              `json:"turns"`
	CompletedTurns         int              `json:"completed_turns"`
	FailedTurns            int              `json:"failed_turns"`
	IncompleteTurns        int              `json:"incomplete_turns"`
	Tokens                 *TokenUsage      `json:"tokens"`
	TokenStatus            TokenStatus      `json:"token_status"`
	KnownTokenTurns        int              `json:"known_token_turns"`
	UnknownTokenTurns      int              `json:"unknown_token_turns"`
	InferenceMS            int64            `json:"inference_ms"`
	MeasuredInferenceTurns int              `json:"measured_inference_turns"`
	UnknownInferenceTurns  int              `json:"unknown_inference_turns"`
	RecordingOverheadMS    int64            `json:"recording_overhead_ms"`
}

// PromptIdentity identifies the fixed task prompt used by a run.
type PromptIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Hash    string `json:"hash"` // SHA-256 of the prompt template content.
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
