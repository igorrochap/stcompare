package bench

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"stcompare/benchrecord"
)

// TextReporter narrates loop progress as human-readable lines, one per phase
// boundary. It is the default reporter for interactive stbench runs so a run
// blocked on a slow adapter is visibly waiting rather than apparently hung.
type TextReporter struct {
	Writer io.Writer
}

// NewTextReporter creates a TextReporter writing to w.
func NewTextReporter(w io.Writer) *TextReporter {
	return &TextReporter{Writer: w}
}

var _ Reporter = (*TextReporter)(nil)

// Report renders one progress event. Unrecognized phases are ignored so future
// events are additive.
func (reporter *TextReporter) Report(event ProgressEvent) {
	if reporter.Writer == nil {
		return
	}
	if line := formatEvent(event); line != "" {
		fmt.Fprintln(reporter.Writer, line)
	}
}

func formatEvent(event ProgressEvent) string {
	prefix := fmt.Sprintf("[iter %d]", event.Iteration)

	switch event.Phase {
	case ProgressPhasePreflight:
		switch event.State {
		case ProgressStart:
			return "preflight: preparing candidate and adapter…"
		case ProgressDone:
			return "preflight: ready"
		case ProgressError:
			return fmt.Sprintf("preflight: failed: %v", event.Err)
		}
	case ProgressPhaseIteration:
		if event.State == ProgressStart {
			return fmt.Sprintf("\n=== iteration %d ===", event.Iteration)
		}
	case ProgressPhaseCompare:
		switch event.State {
		case ProgressStart:
			return prefix + " comparing against baseline…"
		case ProgressDone:
			if event.Converged {
				return prefix + " compare: converged"
			}
			return fmt.Sprintf("%s compare: %d actionable, %d still-failing",
				prefix, event.Actionable, event.StillFailing)
		case ProgressError:
			return fmt.Sprintf("%s compare: failed: %v", prefix, event.Err)
		}
	case ProgressPhaseAgentFix:
		switch event.State {
		case ProgressStart:
			return prefix + " prompting agent to fix… (local models can take minutes)"
		case ProgressWaiting:
			return prefix + " still working… " + waitingDetail(event)
		case ProgressDone:
			return prefix + " agent responded"
		case ProgressError:
			return fmt.Sprintf("%s agent fix: failed: %v", prefix, event.Err)
		}
	case ProgressPhaseTerminal:
		return fmt.Sprintf("%s done: %s", prefix, terminalDescription(event.Terminal))
	default:
		// Candidate lifecycle sub-phases carry a benchrecord.LifecyclePhase value.
		if state := lifecycleState(event); state != "" {
			return prefix + " " + state
		}
	}
	return ""
}

func waitingDetail(event ProgressEvent) string {
	elapsed := fmt.Sprintf("%s elapsed", event.Elapsed.Round(time.Second))
	if len(event.ChangedFiles) == 0 {
		return elapsed
	}
	return fmt.Sprintf("%s — %d file(s) changed: %s",
		elapsed, len(event.ChangedFiles), summarizeFiles(event.ChangedFiles, 3))
}

func summarizeFiles(files []string, limit int) string {
	shown := make([]string, 0, limit)
	for _, file := range files {
		if len(shown) == limit {
			break
		}
		shown = append(shown, filepath.Base(file))
	}
	summary := strings.Join(shown, ", ")
	if extra := len(files) - len(shown); extra > 0 {
		summary += fmt.Sprintf(", +%d more", extra)
	}
	return summary
}

func lifecycleState(event ProgressEvent) string {
	label, ok := lifecycleLabels[benchrecord.LifecyclePhase(event.Phase)]
	if !ok {
		return ""
	}
	switch event.State {
	case ProgressStart:
		return label + "…"
	case ProgressError:
		return fmt.Sprintf("%s failed: %v", label, event.Err)
	default:
		return ""
	}
}

var lifecycleLabels = map[benchrecord.LifecyclePhase]string{
	benchrecord.LifecyclePhaseStop:        "stopping candidate",
	benchrecord.LifecyclePhaseReset:       "resetting candidate",
	benchrecord.LifecyclePhaseBuild:       "building the API under test",
	benchrecord.LifecyclePhaseStart:       "starting candidate",
	benchrecord.LifecyclePhaseWaitHealthy: "waiting for health check",
}

func terminalDescription(state benchrecord.TerminalState) string {
	switch state {
	case benchrecord.TerminalStateConverged:
		return "converged"
	case benchrecord.TerminalStateStalled:
		return "stalled (no progress)"
	case benchrecord.TerminalStateMaxIterations:
		return "max iterations reached"
	default:
		return string(state)
	}
}
