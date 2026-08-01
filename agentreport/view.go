package agentreport

// SchemaVersion identifies the compact agent-view wire schema.
const SchemaVersion = "1"

// ActionKind identifies why an actionable item requires agent attention.
type ActionKind string

const (
	// ActionKindRegressed identifies a newly regressed interaction.
	ActionKindRegressed ActionKind = "regressed"
	// ActionKindStillFailing identifies a baseline problem that remains.
	ActionKindStillFailing ActionKind = "still_failing"
)

// CheckCategory identifies the category associated with an actionable item.
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

// Actionable is a compact pointer to one fixable problem or regression.
type Actionable struct {
	ID            string        `json:"id"`
	Kind          ActionKind    `json:"kind"`
	CheckCategory CheckCategory `json:"check_category"`
	Operation     string        `json:"operation"`
	Status        Status        `json:"status"`
	Message       string        `json:"message"`
	Ref           int           `json:"ref"`
}

// Status contains the baseline and candidate HTTP statuses for an item.
type Status struct {
	Baseline  *int `json:"baseline"`
	Candidate *int `json:"candidate"`
}
