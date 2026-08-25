package bench

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
)

func TestRunConvergesOnFirstIteration(t *testing.T) {
	view := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Converged:     true,
		Candidate:     "candidate",
		Baseline:      "baseline",
		Counts:        agentreport.Counts{Fixed: 2},
	}
	comparator := &fakeComparator{results: []comparisonResult{{view: view, exitCode: agentreport.ExitCodeConverged}}}
	candidate := &fakeCandidate{}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		AdapterMetadata: AdapterMetadata{Agent: "agent"},
		Candidate:       "candidate",
		Baseline:        "baseline",
		BaselineExists:  func() bool { return true },
		MaxIterations:   3,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if record.Iterations != 1 {
		t.Fatalf("iterations = %d, want 1", record.Iterations)
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter calls = %d, want 0", len(adapter.instructions))
	}
	if len(record.RemainingActionable) != 0 {
		t.Fatalf("remaining actionable = %#v, want empty", record.RemainingActionable)
	}
	if got, want := candidate.calls, preflightAndFirstIterationCalls; !sameStrings(got, want) {
		t.Fatalf("candidate calls = %#v, want %#v", got, want)
	}
	if len(comparator.configs) != 1 {
		t.Fatalf("comparator calls = %d, want 1", len(comparator.configs))
	}
}

func TestRunClosesAdapterAndRecordsNegotiatedProcessReuse(t *testing.T) {
	adapter := &trackingAdapter{
		fakeAdapter:  &fakeAdapter{},
		processReuse: true,
	}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		ReuseProcess:   true,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if adapter.closeCalls != 1 {
		t.Fatalf("adapter close calls = %d, want 1", adapter.closeCalls)
	}
	if !record.ProcessReuse {
		t.Fatal("record.ProcessReuse = false, want true")
	}
}

func TestRunKeepsConvergedRecordWhenAdapterCloseFails(t *testing.T) {
	adapter := &trackingAdapter{
		fakeAdapter:  &fakeAdapter{},
		processReuse: true,
		closeErr:     errors.New("adapter process exited on cleanup"),
	}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		ReuseProcess:   true,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("Run() error = %v, want cleanup error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want converged", record.TerminalState)
	}
}

func TestRunRecordsColdFallbackWhenAdapterDoesNotNegotiateReuse(t *testing.T) {
	adapter := &trackingAdapter{fakeAdapter: &fakeAdapter{}, processReuse: false}
	comparator := &fakeComparator{results: []comparisonResult{{
		view:     agentreport.View{Converged: true},
		exitCode: agentreport.ExitCodeConverged,
	}}}

	record, err := Run(Config{
		ReuseProcess:   true,
		BaselineExists: func() bool { return true },
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if record.ProcessReuse {
		t.Fatal("record.ProcessReuse = true, want cold fallback")
	}
}

func TestRunPreflightsLifecycleAndAdapterBeforeComparison(t *testing.T) {
	view := agentreport.View{
		Converged: true,
		Counts:    agentreport.Counts{Fixed: 1},
	}
	comparator := &fakeComparator{results: []comparisonResult{{view: view, exitCode: agentreport.ExitCodeConverged}}}
	candidate := &fakeCandidate{}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  1,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if len(adapter.preflightMetadata) != 1 {
		t.Fatalf("adapter preflight calls = %d, want 1", len(adapter.preflightMetadata))
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter fix calls = %d, want 0 for a converged comparison", len(adapter.instructions))
	}
	if len(comparator.configs) != 1 {
		t.Fatalf("comparator calls = %d, want 1 after preflight", len(comparator.configs))
	}
	if got, want := candidate.calls, preflightAndFirstIterationCalls; !sameStrings(got, want) {
		t.Fatalf("candidate calls = %#v, want %#v", got, want)
	}
}

func TestRunReportsPreflightLifecycleFailureBeforeComparison(t *testing.T) {
	candidate := &fakeCandidate{failPhase: "build", failErr: errors.New("compile failed")}
	comparator := &fakeComparator{}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "preflight") || !strings.Contains(err.Error(), "build") {
		t.Fatalf("Run error = %v, want preflight build error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateLifecycleError || record.LifecyclePhase != benchrecord.LifecyclePhaseBuild {
		t.Fatalf("record = %#v, want preflight build lifecycle error", record)
	}
	if record.Iterations != 0 || len(comparator.configs) != 0 {
		t.Fatalf("preflight failure performed comparison work: iterations=%d comparisons=%d", record.Iterations, len(comparator.configs))
	}
}

func TestRunReportsPreflightAdapterFailureBeforeComparison(t *testing.T) {
	comparator := &fakeComparator{}
	adapter := &fakeAdapter{preflightErr: errors.New("adapter command not found")}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "preflight adapter") {
		t.Fatalf("Run error = %v, want preflight adapter error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateAdapterError)
	}
	if record.Iterations != 0 || len(comparator.configs) != 0 || len(adapter.instructions) != 0 {
		t.Fatalf("preflight failure performed real work: iterations=%d comparisons=%d fixes=%d", record.Iterations, len(comparator.configs), len(adapter.instructions))
	}
}

func TestRunIteratesThenConvergesAndPassesRenderedPrompt(t *testing.T) {
	first := agentreport.View{
		Converged: false,
		Counts:    agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	second := first
	third := agentreport.View{Converged: true, Counts: agentreport.Counts{Fixed: 1}}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
		{view: third, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{usages: []*benchrecord.TokenUsage{
		{Input: 1, Output: 2, Total: 3},
		{Input: 4, Output: 5, Total: 9},
	}}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if record.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", record.Iterations)
	}
	if len(adapter.instructions) != 2 {
		t.Fatalf("adapter calls = %d, want 2", len(adapter.instructions))
	}
	if !strings.Contains(adapter.instructions[0], `"problem-1"`) {
		t.Fatalf("rendered instruction does not contain the compact view: %q", adapter.instructions[0])
	}
	if !strings.Contains(adapter.instructions[0], "stbench-default@2") {
		t.Fatalf("rendered instruction does not contain the prompt identity: %q", adapter.instructions[0])
	}
	if got, want := *record.Tokens, (benchrecord.TokenUsage{Input: 5, Output: 7, Total: 12}); got != want {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
	if record.Final.StillFailing != 0 || record.Final.Converged != true {
		t.Fatalf("final summary = %#v, want converged with no remaining failures", record.Final)
	}
}

func TestRunRecordsPromptHashAndRenderedInstructions(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{responses: []string{"raw model response"}}

	record, err := Run(testConfig(), Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(record.Prompt.Hash) != 64 {
		t.Fatalf("prompt hash length = %d, want SHA-256 hex length", len(record.Prompt.Hash))
	}
	wantPromptHash := hashContent(promptTemplateText)
	if record.Prompt.Hash != wantPromptHash {
		t.Fatalf("prompt hash = %q, want embedded template hash %q", record.Prompt.Hash, wantPromptHash)
	}
	if len(record.PromptInstructions) != 1 {
		t.Fatalf("prompt instructions = %#v, want one rendered instruction", record.PromptInstructions)
	}
	if record.PromptInstructions[0] != adapter.instructions[0] {
		t.Fatalf("archived instruction = %q, want adapter instruction %q", record.PromptInstructions[0], adapter.instructions[0])
	}
	if strings.Contains(record.PromptInstructions[0], record.Prompt.Hash) {
		t.Fatalf("rendered instruction should not contain audit metadata: %q", record.PromptInstructions[0])
	}
	if len(record.RenderedPromptHashes) != 1 || len(record.RenderedPromptHashes[0]) != 64 {
		t.Fatalf("rendered prompt hashes = %#v, want one SHA-256 hash", record.RenderedPromptHashes)
	}
	if len(record.AgentResponses) != 1 || record.AgentResponses[0] != "raw model response" {
		t.Fatalf("agent responses = %#v, want archived raw model response", record.AgentResponses)
	}
}

func TestRunUsesCustomPromptFileFromWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	promptContent := "Custom\n{{ .ComparisonView }}\n"
	if err := os.WriteFile(filepath.Join(directory, "prompt.md"), []byte(promptContent), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}
	var notice bytes.Buffer
	config := testConfig()
	config.PromptFile = "prompt.md"

	record, err := Run(config, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
		Notice:     &notice,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := record.Prompt.Hash, "d7d38aa643f66b523788c3b0257575a6172ff98dc10dde81f883a0c828681160"; got != want {
		t.Fatalf("prompt hash = %q, want file content hash %q", got, want)
	}
	if len(adapter.instructions) != 1 || !strings.HasPrefix(adapter.instructions[0], "Custom\n") ||
		!strings.Contains(adapter.instructions[0], `"problem-1"`) {
		t.Fatalf("adapter instructions = %#v, want custom template rendered with comparison view", adapter.instructions)
	}
	if got, want := notice.String(), "using custom prompt template prompt.md\n"; got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

func TestRunRejectsInvalidCustomPromptBeforeLoop(t *testing.T) {
	directory := t.TempDir()
	tests := []struct {
		name        string
		path        string
		content     string
		wantMessage string
		writeFile   bool
	}{
		{
			name:        "missing file",
			path:        filepath.Join(directory, "missing.md"),
			wantMessage: "load custom prompt template",
		},
		{
			name:        "whitespace-only path",
			path:        "   ",
			wantMessage: "load custom prompt template",
		},
		{
			name:        "unparseable template",
			path:        filepath.Join(directory, "unparseable.md"),
			content:     "{{",
			wantMessage: "parse custom prompt template",
			writeFile:   true,
		},
		{
			name:        "missing comparison view",
			path:        filepath.Join(directory, "no-view.md"),
			content:     "{{/* .ComparisonView */}}Fix the candidate",
			wantMessage: "must reference .ComparisonView",
			writeFile:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.writeFile {
				if err := os.WriteFile(test.path, []byte(test.content), 0o644); err != nil {
					t.Fatalf("write prompt file: %v", err)
				}
			}
			comparator := &fakeComparator{}
			candidate := &fakeCandidate{}
			adapter := &fakeAdapter{}
			config := testConfig()
			config.PromptFile = test.path

			record, err := Run(config, Dependencies{
				Comparator: comparator,
				Candidate:  candidate,
				Adapter:    adapter,
			})
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("Run error = %v, want message containing %q", err, test.wantMessage)
			}
			if record.SchemaVersion != "" || len(comparator.configs) != 0 ||
				len(candidate.calls) != 0 || len(adapter.preflightMetadata) != 0 {
				t.Fatalf("invalid prompt started loop: record=%#v comparisons=%d candidate=%#v preflights=%d",
					record, len(comparator.configs), candidate.calls, len(adapter.preflightMetadata))
			}
		})
	}
}

func TestRunEmbeddedPromptDoesNotEmitCustomPromptNotice(t *testing.T) {
	var notice bytes.Buffer
	_, err := Run(Config{BaselineExists: func() bool { return true }}, Dependencies{
		Comparator: &fakeComparator{results: []comparisonResult{{
			view:     agentreport.View{Converged: true},
			exitCode: agentreport.ExitCodeConverged,
		}}},
		Candidate: &fakeCandidate{},
		Adapter:   &fakeAdapter{},
		Notice:    &notice,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if notice.Len() != 0 {
		t.Fatalf("notice = %q, want silence for embedded prompt", notice.String())
	}
}

func TestRunPassesRecordedMetadataToAdapter(t *testing.T) {
	view := agentreport.View{Counts: agentreport.Counts{StillFailing: 1}}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: agentreport.View{Converged: true}, exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{}

	_, err := Run(Config{
		AdapterMetadata: AdapterMetadata{Agent: "codex", Model: "gpt-5", Effort: "high", Hardware: "m4-pro"},
		BaselineExists:  func() bool { return true },
		MaxIterations:   2,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    adapter,
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(adapter.metadata) != 1 {
		t.Fatalf("adapter metadata calls = %d, want 1", len(adapter.metadata))
	}
	want := AdapterMetadata{Agent: "codex", Model: "gpt-5", Effort: "high", Hardware: "m4-pro"}
	if adapter.metadata[0] != want {
		t.Fatalf("adapter metadata = %#v, want %#v", adapter.metadata[0], want)
	}
}

func TestRunStopsAtMaxIterations(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
	}}
	adapter := &fakeAdapter{}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  2,
	}, Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0))})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateMaxIterations {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateMaxIterations)
	}
	if len(adapter.instructions) != 1 {
		t.Fatalf("adapter calls = %d, want 1", len(adapter.instructions))
	}
	if len(record.RemainingActionable) != 1 || record.RemainingActionable[0].ID != "problem-1" {
		t.Fatalf("remaining actionable = %#v, want problem-1", record.RemainingActionable)
	}
}

func TestRunStopsOnStallAndMarksPersistentActionableItems(t *testing.T) {
	view := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
		{view: view, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateStalled)
	}
	if record.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", record.Iterations)
	}
	if len(record.RemainingActionable) != 1 {
		t.Fatalf("remaining actionable = %#v, want one item", record.RemainingActionable)
	}
	if item := record.RemainingActionable[0]; item.ID != "problem-1" || !item.Stuck {
		t.Fatalf("remaining actionable item = %#v, want persistent stuck item", item)
	}
}

func TestRunRejectsNegativeStallWindow(t *testing.T) {
	_, err := Run(Config{
		BaselineExists: func() bool { return true },
		StallWindow:    -1,
	}, Dependencies{
		Comparator: &fakeComparator{},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
	})
	if err == nil || !strings.Contains(err.Error(), "stall window") {
		t.Fatalf("Run error = %v, want negative stall-window error", err)
	}
}

func TestRunMarksNewlyIntroducedActionableItemsAsNotStuck(t *testing.T) {
	first := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	regressed := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1, Regressed: 1},
		Actionable: []agentreport.Actionable{
			{ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets"},
			{ID: "regression-1", Kind: agentreport.ActionKindRegressed, Operation: "POST /widgets"},
		},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: regressed, exitCode: agentreport.ExitCodeNotConverged},
		{view: regressed, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
		StallWindow:    2,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateStalled)
	}
	if len(record.RemainingActionable) != 2 {
		t.Fatalf("remaining actionable = %#v, want two items", record.RemainingActionable)
	}
	if !record.RemainingActionable[0].Stuck || record.RemainingActionable[1].Stuck {
		t.Fatalf("remaining actionable = %#v, want persistent then newly introduced", record.RemainingActionable)
	}
}

func TestRunWithDecreasingCountsConvergesWithoutStalling(t *testing.T) {
	first := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 2},
		Actionable: []agentreport.Actionable{
			{ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets"},
			{ID: "problem-2", Kind: agentreport.ActionKindStillFailing, Operation: "POST /widgets"},
		},
	}
	second := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	third := agentreport.View{Converged: true}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
		{view: third, exitCode: agentreport.ExitCodeConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  3,
		StallWindow:    2,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateConverged {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateConverged)
	}
	if record.Iterations != 3 {
		t.Fatalf("iterations = %d, want 3", record.Iterations)
	}
}

func TestRunMaxIterationsMarksPersistentAndNewItems(t *testing.T) {
	first := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets",
		}},
	}
	second := agentreport.View{
		Counts: agentreport.Counts{StillFailing: 1, Regressed: 1},
		Actionable: []agentreport.Actionable{
			{ID: "problem-1", Kind: agentreport.ActionKindStillFailing, Operation: "GET /widgets"},
			{ID: "regression-1", Kind: agentreport.ActionKindRegressed, Operation: "POST /widgets"},
		},
	}
	comparator := &fakeComparator{results: []comparisonResult{
		{view: first, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
		{view: second, exitCode: agentreport.ExitCodeNotConverged},
	}}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  3,
		StallWindow:    5,
	}, Dependencies{
		Comparator: comparator,
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateMaxIterations {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateMaxIterations)
	}
	if !record.RemainingActionable[0].Stuck || record.RemainingActionable[1].Stuck {
		t.Fatalf("remaining actionable = %#v, want persistent then newly introduced", record.RemainingActionable)
	}
}

func TestRunMaxIterationsRemainsReachableForOscillatingCounts(t *testing.T) {
	views := []agentreport.View{
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}}},
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}, {ID: "problem-2"}}},
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}}},
		{Actionable: []agentreport.Actionable{{ID: "problem-1"}, {ID: "problem-2"}}},
	}
	results := make([]comparisonResult, len(views))
	for i, view := range views {
		results[i] = comparisonResult{view: view, exitCode: agentreport.ExitCodeNotConverged}
	}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  4,
		StallWindow:    3,
	}, Dependencies{
		Comparator: &fakeComparator{results: results},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateMaxIterations {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateMaxIterations)
	}
}

func TestRunLargerStallWindowDelaysStall(t *testing.T) {
	view := agentreport.View{Actionable: []agentreport.Actionable{{ID: "problem-1"}}}
	results := make([]comparisonResult, 4)
	for i := range results {
		results[i] = comparisonResult{view: view, exitCode: agentreport.ExitCodeNotConverged}
	}

	record, err := Run(Config{
		BaselineExists: func() bool { return true },
		MaxIterations:  5,
		StallWindow:    3,
	}, Dependencies{
		Comparator: &fakeComparator{results: results},
		Candidate:  &fakeCandidate{},
		Adapter:    &fakeAdapter{},
		Now:        fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateStalled {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateStalled)
	}
	if record.Iterations != 4 {
		t.Fatalf("iterations = %d, want 4", record.Iterations)
	}
}

func TestRunStopsOnComparatorToolError(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{exitCode: agentreport.ExitCodeToolError}}}
	adapter := &fakeAdapter{}

	record, err := Run(testConfig(), Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0))})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.TerminalState != benchrecord.TerminalStateToolError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateToolError)
	}
	if len(adapter.instructions) != 0 {
		t.Fatalf("adapter calls = %d, want 0", len(adapter.instructions))
	}
}

func TestRunStopsOnAdapterError(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{{exitCode: agentreport.ExitCodeNotConverged}}}
	adapter := &fakeAdapter{
		responses: []string{"partial raw model response"},
		errs:      []error{errors.New("agent failed")},
	}

	record, err := Run(testConfig(), Dependencies{Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0))})
	if err == nil || !strings.Contains(err.Error(), "agent failed") {
		t.Fatalf("Run error = %v, want adapter error", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateAdapterError)
	}
	if len(record.AgentResponses) != 1 || record.AgentResponses[0] != "partial raw model response" {
		t.Fatalf("agent responses = %#v, want archived response from failed adapter", record.AgentResponses)
	}
}

func TestRunRecordsFailedCandidatePhase(t *testing.T) {
	candidate := &fakeCandidate{failPhase: "build", failErr: errors.New("compile failed")}
	record, err := Run(testConfig(), Dependencies{
		Comparator: &fakeComparator{}, Candidate: candidate, Adapter: &fakeAdapter{}, Now: fixedNow(time.Unix(0, 0)),
	})
	if err == nil || !strings.Contains(err.Error(), "build") {
		t.Fatalf("Run error = %v, want build phase", err)
	}
	if record.TerminalState != benchrecord.TerminalStateLifecycleError || record.LifecyclePhase != "build" {
		t.Fatalf("lifecycle failure = state %q phase %q", record.TerminalState, record.LifecyclePhase)
	}
}

func TestRunTurnsLifecycleTimeoutIntoLifecycleError(t *testing.T) {
	candidate := &CommandCandidate{
		BuildCommand:   "sleep 1",
		CommandTimeout: 10 * time.Millisecond,
	}

	record, err := Run(testConfig(), Dependencies{
		Comparator: &fakeComparator{},
		Candidate:  candidate,
		Adapter:    &fakeAdapter{},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run error = %v, want lifecycle timeout", err)
	}
	if record.TerminalState != benchrecord.TerminalStateLifecycleError || record.LifecyclePhase != "build" {
		t.Fatalf("record = %#v, want build lifecycle error", record)
	}
}

func TestRunTurnsAdapterTimeoutIntoAdapterError(t *testing.T) {
	record, err := Run(testConfig(), Dependencies{
		Comparator: &fakeComparator{results: []comparisonResult{{
			view:     agentreport.View{Counts: agentreport.Counts{StillFailing: 1}},
			exitCode: agentreport.ExitCodeNotConverged,
		}}},
		Candidate: &fakeCandidate{},
		Adapter: &CommandAdapter{
			Command:        "sleep 1",
			CommandTimeout: 10 * time.Millisecond,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run error = %v, want adapter timeout", err)
	}
	if record.TerminalState != benchrecord.TerminalStateAdapterError {
		t.Fatalf("terminal state = %q, want %q", record.TerminalState, benchrecord.TerminalStateAdapterError)
	}
}

func TestRunMissingBaselineFailsBeforeCandidateLifecycle(t *testing.T) {
	candidate := &fakeCandidate{}
	comparator := &fakeComparator{}
	record, err := Run(Config{
		Baseline:       "baseline",
		BaselineExists: func() bool { return false },
		MaxIterations:  2,
	}, Dependencies{Comparator: comparator, Candidate: candidate, Adapter: &fakeAdapter{}, Now: fixedNow(time.Unix(0, 0))})
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("Run error = %v, want missing baseline error", err)
	}
	if record.Iterations != 0 || len(candidate.calls) != 0 || len(comparator.configs) != 0 {
		t.Fatalf("missing baseline performed work: iterations=%d candidate=%#v comparator=%#v", record.Iterations, candidate.calls, comparator.configs)
	}
	if record.LifecyclePhase != benchrecord.LifecyclePhaseBaselinePrecondition {
		t.Fatalf("lifecycle phase = %q, want %q", record.LifecyclePhase, benchrecord.LifecyclePhaseBaselinePrecondition)
	}
}

func TestRunTimeBreakdownSumsAndPartialTokens(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{usages: []*benchrecord.TokenUsage{{Input: 3, Output: 4, Total: 7}, nil}}
	now := advancingNow(10 * time.Millisecond)

	record, err := Run(Config{BaselineExists: func() bool { return true }, MaxIterations: 3}, Dependencies{
		Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: now,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := *record.Tokens, (benchrecord.TokenUsage{Input: 3, Output: 4, Total: 7}); got != want {
		t.Fatalf("tokens = %#v, want known partial sum %#v", got, want)
	}
	if got, want := record.UnknownTokenIterations, 1; got != want {
		t.Fatalf("unknown token iterations = %d, want %d", got, want)
	}
	if got := record.TimeMS.Total; got != record.TimeMS.CandidateReset+record.TimeMS.Compare+record.TimeMS.AgentFix {
		t.Fatalf("time total = %d, phase sum = %d", got, record.TimeMS.CandidateReset+record.TimeMS.Compare+record.TimeMS.AgentFix)
	}
}

func TestRunKeepsTokensNullWhenEveryFixOmitsTokenUsage(t *testing.T) {
	comparator := &fakeComparator{results: []comparisonResult{
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeNotConverged},
		{exitCode: agentreport.ExitCodeConverged},
	}}
	adapter := &fakeAdapter{usages: []*benchrecord.TokenUsage{nil, nil}}

	record, err := Run(Config{BaselineExists: func() bool { return true }, MaxIterations: 3}, Dependencies{
		Comparator: comparator, Candidate: &fakeCandidate{}, Adapter: adapter, Now: fixedNow(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if record.Tokens != nil {
		t.Fatalf("tokens = %#v, want null when every fix omits usage", record.Tokens)
	}
	if got, want := record.UnknownTokenIterations, 2; got != want {
		t.Fatalf("unknown token iterations = %d, want %d", got, want)
	}
}

type comparisonResult struct {
	view     agentreport.View
	exitCode int
	err      error
}

type fakeComparator struct {
	results []comparisonResult
	configs []Config
}

func (f *fakeComparator) Compare(config Config) (agentreport.View, int, error) {
	f.configs = append(f.configs, config)
	result := f.results[0]
	f.results = f.results[1:]
	return result.view, result.exitCode, result.err
}

type fakeCandidate struct {
	calls     []string
	failPhase string
	failErr   error
}

func (f *fakeCandidate) Stop() error {
	f.calls = append(f.calls, "stop")
	return f.fail("stop")
}

func (f *fakeCandidate) Reset() error {
	f.calls = append(f.calls, "reset")
	return f.fail("reset")
}

func (f *fakeCandidate) Build() error {
	f.calls = append(f.calls, "build")
	return f.fail("build")
}

func (f *fakeCandidate) Start() error {
	f.calls = append(f.calls, "start")
	return f.fail("start")
}

func (f *fakeCandidate) WaitHealthy() error {
	f.calls = append(f.calls, "wait_healthy")
	return f.fail("wait_healthy")
}

func (f *fakeCandidate) fail(phase string) error {
	if f.failPhase == phase {
		return f.failErr
	}
	return nil
}

type fakeAdapter struct {
	preflightMetadata []AdapterMetadata
	preflightErr      error
	instructions      []string
	metadata          []AdapterMetadata
	usages            []*benchrecord.TokenUsage
	responses         []string
	errs              []error
}

type trackingAdapter struct {
	*fakeAdapter
	closeCalls   int
	processReuse bool
	closeErr     error
}

func (adapter *trackingAdapter) Close() error {
	adapter.closeCalls++
	return adapter.closeErr
}

func (adapter *trackingAdapter) ProcessReuseActive() bool {
	return adapter.processReuse
}

func (f *fakeAdapter) Preflight(metadata AdapterMetadata) error {
	f.preflightMetadata = append(f.preflightMetadata, metadata)
	return f.preflightErr
}

func (f *fakeAdapter) Fix(
	instruction string,
	_ agentreport.View,
	metadata AdapterMetadata,
) (*AdapterResult, error) {
	f.instructions = append(f.instructions, instruction)
	f.metadata = append(f.metadata, metadata)
	var usage *benchrecord.TokenUsage
	if len(f.usages) != 0 {
		usage = f.usages[0]
		f.usages = f.usages[1:]
	}
	var response string
	if len(f.responses) != 0 {
		response = f.responses[0]
		f.responses = f.responses[1:]
	}
	var err error
	if len(f.errs) != 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return &AdapterResult{Tokens: usage, Response: response}, err
}

func testConfig() Config {
	return Config{BaselineExists: func() bool { return true }, MaxIterations: 5, StallWindow: 3}
}

var preflightAndFirstIterationCalls = []string{
	"stop", "reset", "build", "start", "wait_healthy", "stop",
	"stop", "reset", "build", "start", "wait_healthy",
}

func fixedNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func advancingNow(step time.Duration) func() time.Time {
	now := time.Unix(0, 0)
	return func() time.Time {
		current := now
		now = now.Add(step)
		return current
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
