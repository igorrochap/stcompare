// Package agentreport contains the public contracts consumed by agent
// orchestration tools.
package agentreport

// Exit codes returned by campaign compare.
const (
	ExitCodeConverged    = 0
	ExitCodeToolError    = 1
	ExitCodeNotConverged = 2
)
