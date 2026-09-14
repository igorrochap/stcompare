package agentreport

// SchemaVersion identifies the compact agent-view wire schema.
const SchemaVersion = "2"

// ActionKind identifies why a Problem Group requires agent attention.
type ActionKind string

const (
	// ActionKindRegressed identifies a newly regressed interaction.
	ActionKindRegressed ActionKind = "regressed"
	// ActionKindStillFailing identifies a baseline problem that remains.
	ActionKindStillFailing ActionKind = "still_failing"
)

// CheckCategory identifies the category associated with a Problem Group.
type CheckCategory string

const (
	// CheckCategoryTrafficRegression identifies a regression rather than a
	// Schemathesis check category.
	CheckCategoryTrafficRegression CheckCategory = "traffic_regression"
)

// View is the compact, machine-readable projection of a campaign comparison.
type View struct {
	SchemaVersion string       `json:"schema_version"`
	Converged     bool         `json:"converged"`
	Candidate     string       `json:"candidate"`
	Baseline      string       `json:"baseline"`
	Counts        Counts       `json:"counts"`
	Unverified    Unverified   `json:"unverified"`
	Actionable    []Actionable `json:"actionable"`
}

// Counts contains the progress and convergence-driving counts.
type Counts struct {
	Fixed        int `json:"fixed"`
	StillFailing int `json:"still_failing"`
	Regressed    int `json:"regressed"`
}

// Unverified contains problem counts that cannot be resolved by endpoint code.
type Unverified struct {
	Inconclusive int `json:"inconclusive"`
	Uncorrelated int `json:"uncorrelated"`
	Ambiguous    int `json:"ambiguous"`
	Unevaluable  int `json:"unevaluable"`
}

// Actionable is a compact Problem Group containing bounded evidence.
type Actionable struct {
	ID            string        `json:"id"`
	Kind          ActionKind    `json:"kind"`
	CheckCategory CheckCategory `json:"check_category"`
	Operation     string        `json:"operation"`
	Status        Status        `json:"status"`
	Message       string        `json:"message"`
	Count         int           `json:"count"`
	Refs          []int         `json:"refs"`
	Sample        Sample        `json:"sample"`
}

// Sample is bounded evidence for the lowest-reference interaction in a
// Problem Group.
type Sample struct {
	Ref          int      `json:"ref"`
	Operation    string   `json:"operation"`
	RequestBody  string   `json:"request_body"`
	ResponseBody string   `json:"response_body"`
	Details      []string `json:"details"`
}

// Status contains the baseline and candidate HTTP statuses for a Problem Group.
type Status struct {
	Baseline  *int `json:"baseline"`
	Candidate *int `json:"candidate"`
}
