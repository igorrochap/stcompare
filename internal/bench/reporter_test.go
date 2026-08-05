package bench

import (
	"strings"
	"testing"
	"time"

	"stcompare/agentreport"
)

type recordingReporter struct {
	events []ProgressEvent
}

func (reporter *recordingReporter) Report(event ProgressEvent) {
	reporter.events = append(reporter.events, event)
}

func TestRunNarratesPhasesForOneFixThenConverge(t *testing.T) {
	notConverged := agentreport.View{
		Actionable: []agentreport.Actionable{{ID: "a"}, {ID: "b"}},
		Counts:     agentreport.Counts{StillFailing: 2},
	}
	converged := agentreport.View{Converged: true}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: notConverged, exitCode: agentreport.ExitCodeNotConverged},
		{view: converged, exitCode: agentreport.ExitCodeConverged},
	}}
	reporter := &recordingReporter{}

	_, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
		Reporter:   reporter,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Preflight is narrated once, before any iteration.
	if got := reporter.events[0]; got.Phase != ProgressPhasePreflight || got.State != ProgressStart {
		t.Fatalf("first event = %+v, want preflight start", got)
	}

	compareDone := findEvent(reporter.events, ProgressPhaseCompare, ProgressDone, 1)
	if compareDone == nil {
		t.Fatal("missing compare-done event for iteration 1")
	}
	if compareDone.Actionable != 2 || compareDone.StillFailing != 2 || compareDone.Converged {
		t.Fatalf("iteration 1 compare event = %+v, want 2 actionable / 2 still-failing / not converged", *compareDone)
	}

	if findEvent(reporter.events, ProgressPhaseAgentFix, ProgressStart, 1) == nil {
		t.Fatal("missing agent-fix start event for iteration 1")
	}
	if findEvent(reporter.events, ProgressPhaseAgentFix, ProgressDone, 1) == nil {
		t.Fatal("missing agent-fix done event for iteration 1")
	}

	// Iteration 2 converges, so it must not prompt the agent again.
	if findEvent(reporter.events, ProgressPhaseAgentFix, ProgressStart, 2) != nil {
		t.Fatal("agent should not be prompted on the converged iteration")
	}
	terminal := findEvent(reporter.events, ProgressPhaseTerminal, ProgressDone, 2)
	if terminal == nil {
		t.Fatal("missing terminal event for iteration 2")
	}
}

type blockingAdapter struct {
	block time.Duration
}

func (adapter *blockingAdapter) Preflight(AdapterMetadata) error { return nil }

func (adapter *blockingAdapter) Fix(string, agentreport.View, AdapterMetadata) (*AdapterResult, error) {
	time.Sleep(adapter.block)
	return &AdapterResult{Response: "done"}, nil
}

type staticInspector struct {
	files []string
}

func (inspector staticInspector) Changed() ([]string, error) { return inspector.files, nil }

func TestRunEmitsHeartbeatsWhileAgentIsWorking(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{
		{view: agentreport.View{Actionable: []agentreport.Actionable{{ID: "a"}}}, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	reporter := &recordingReporter{}

	_, err := Run(Config{
		BaselineExists:    func() bool { return true },
		MaxIterations:     5,
		HeartbeatInterval: 5 * time.Millisecond,
	}, Dependencies{
		Comparator:      comparator,
		Candidate:       &fakeCandidate{},
		Adapter:         &blockingAdapter{block: 40 * time.Millisecond},
		Reporter:        reporter,
		ChangeInspector: staticInspector{files: []string{"src/Foo.java", "src/Bar.java"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	waiting := findEvent(reporter.events, ProgressPhaseAgentFix, ProgressWaiting, 1)
	if waiting == nil {
		t.Fatal("expected at least one heartbeat while the agent was working")
	}
	if len(waiting.ChangedFiles) != 2 {
		t.Fatalf("heartbeat changed files = %#v, want 2", waiting.ChangedFiles)
	}
	if waiting.Elapsed <= 0 {
		t.Fatalf("heartbeat elapsed = %v, want positive", waiting.Elapsed)
	}
}

func TestTextReporterMentionsSlowAdapterWait(t *testing.T) {
	var builder strings.Builder
	reporter := NewTextReporter(&builder)
	reporter.Report(ProgressEvent{Iteration: 1, Phase: ProgressPhaseAgentFix, State: ProgressStart})
	if !strings.Contains(builder.String(), "minutes") {
		t.Fatalf("agent-fix narration = %q, want a slow-wait hint", builder.String())
	}
}

func findEvent(events []ProgressEvent, phase, state string, iteration int) *ProgressEvent {
	for i := range events {
		if events[i].Phase == phase && events[i].State == state && events[i].Iteration == iteration {
			return &events[i]
		}
	}
	return nil
}
