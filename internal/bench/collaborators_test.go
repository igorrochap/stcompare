package bench

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
)

func TestCommandComparatorParsesAgentViewAndExitCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := writeExecutable(t, dir, "compare.sh", "#!/bin/sh\n"+
		"printf '%s\\n' \"$@\" > \"$STBENCH_ARGS\"\n"+
		"printf '%s' '{\"schema_version\":\"1\",\"converged\":false,\"candidate\":\"candidate\",\"baseline\":\"baseline\",\"counts\":{\"fixed\":0,\"still_failing\":1,\"regressed\":0},\"unverified\":{\"inconclusive\":0,\"uncorrelated\":0,\"ambiguous\":0,\"unevaluable\":0},\"actionable\":[]}'\n"+
		"exit 2\n")

	comparator := &CommandComparator{
		Binary:     script,
		ConfigPath: filepath.Join(dir, "stcompare.yaml"),
		BaseURL:    "http://candidate.test",
		Env:        []string{"STBENCH_ARGS=" + argsPath},
	}
	view, exitCode, err := comparator.Compare(Config{Candidate: "candidate"})
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if exitCode != agentreport.ExitCodeNotConverged {
		t.Fatalf("exit code = %d, want %d", exitCode, agentreport.ExitCodeNotConverged)
	}
	if view.Candidate != "candidate" || view.Baseline != "baseline" || view.Counts.StillFailing != 1 {
		t.Fatalf("view = %#v, want parsed compact view", view)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read comparator args: %v", err)
	}
	gotArgs := strings.Fields(string(args))
	wantArgs := []string{
		"--config", filepath.Join(dir, "stcompare.yaml"),
		"campaign", "compare", "candidate", "--format", "agent",
		"--base-url", "http://candidate.test",
	}
	if !sameStrings(gotArgs, wantArgs) {
		t.Fatalf("comparator args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestCommandAdapterSendsMetadataInstructionAndViewAndReadsTokens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	pwdPath := filepath.Join(dir, "pwd")
	script := writeExecutable(t, dir, "adapter.sh", "#!/bin/sh\n"+
		"cat > \"$STBENCH_INPUT\"\n"+
		"pwd > \"$STBENCH_PWD\"\n"+
		"printf '%s' '{\"status\":\"ok\",\"response\":\"raw model response\",\"tokens\":{\"input\":11,\"output\":7,\"total\":18}}'\n")
	adapter := &CommandAdapter{
		Command:    script,
		WorkingDir: dir,
		Env:        []string{"STBENCH_INPUT=" + inputPath, "STBENCH_PWD=" + pwdPath},
	}
	wantView := agentreport.View{
		SchemaVersion: agentreport.SchemaVersion,
		Counts:        agentreport.Counts{StillFailing: 1},
		Actionable: []agentreport.Actionable{{
			ID:   "problem-1",
			Kind: agentreport.ActionKindStillFailing,
		}},
	}

	metadata := AdapterMetadata{
		Agent:    "codex",
		Model:    "gpt-5",
		Hardware: "m4-pro",
	}
	result, err := adapter.Fix("fix the candidate", wantView, metadata)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}
	if got, want := *result.Tokens, (benchrecord.TokenUsage{Input: 11, Output: 7, Total: 18}); got != want {
		t.Fatalf("usage = %#v, want %#v", got, want)
	}
	if result.Response != "raw model response" {
		t.Fatalf("response = %q, want raw model response", result.Response)
	}

	contents, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read adapter input: %v", err)
	}
	var request AdapterRequest
	if err := json.Unmarshal(contents, &request); err != nil {
		t.Fatalf("decode adapter input: %v", err)
	}
	if request.Instruction != "fix the candidate" {
		t.Fatalf("instruction = %q, want %q", request.Instruction, "fix the candidate")
	}
	if request.Agent != metadata.Agent || request.Model != metadata.Model ||
		request.Hardware != metadata.Hardware {
		t.Fatalf("metadata = %#v, want %#v", request, metadata)
	}
	if request.View.Counts != wantView.Counts || len(request.View.Actionable) != 1 ||
		request.View.Actionable[0].ID != "problem-1" {
		t.Fatalf("view = %#v, want %#v", request.View, wantView)
	}
	pwd, err := os.ReadFile(pwdPath)
	if err != nil {
		t.Fatalf("read adapter working directory: %v", err)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve adapter working directory: %v", err)
	}
	if got := strings.TrimSpace(string(pwd)); got != resolvedDir {
		t.Fatalf("adapter working directory = %q, want %q", got, resolvedDir)
	}
}

func TestCommandAdapterPreflightSendsNoOpRequest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "preflight.json")
	script := writeExecutable(t, dir, "adapter.sh", "#!/bin/sh\n"+
		"cat > \"$STBENCH_PREFLIGHT\"\n"+
		"printf '%s' '{\"status\":\"ok\",\"tokens\":null}'\n")
	adapter := &CommandAdapter{
		Command:    script,
		WorkingDir: dir,
		Env:        []string{"STBENCH_PREFLIGHT=" + inputPath},
	}
	metadata := AdapterMetadata{Agent: "codex", Model: "gpt-5", Hardware: "m4-pro"}

	if err := adapter.Preflight(metadata); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}

	contents, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read preflight input: %v", err)
	}
	var request AdapterPreflightRequest
	if err := json.Unmarshal(contents, &request); err != nil {
		t.Fatalf("decode preflight input: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		t.Fatalf("decode preflight fields: %v", err)
	}
	if !request.Preflight {
		t.Fatal("preflight request flag = false, want true")
	}
	if _, ok := fields["instruction"]; ok {
		t.Fatal("preflight request contains an instruction")
	}
	if _, ok := fields["view"]; ok {
		t.Fatal("preflight request contains a view")
	}
	if request.Agent != metadata.Agent || request.Model != metadata.Model || request.Hardware != metadata.Hardware {
		t.Fatalf("preflight metadata = %#v, want %#v", request, metadata)
	}
}

func TestCommandAdapterMapsErrorStatusToAdapterError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := writeExecutable(t, dir, "adapter.sh", "#!/bin/sh\n"+
		"printf '%s' '{\"status\":\"error\",\"message\":\"cannot edit\",\"tokens\":null}'\n")
	adapter := &CommandAdapter{Command: script, WorkingDir: dir}

	result, err := adapter.Fix("instruction", agentreport.View{}, AdapterMetadata{})
	if err == nil || !strings.Contains(err.Error(), "cannot edit") {
		t.Fatalf("Fix() error = %v, want adapter message", err)
	}
	if result == nil || result.Tokens != nil {
		t.Fatalf("result = %#v, want result with nil tokens", result)
	}
}

func TestCommandAdapterTimeoutKillsProcessGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	startedPath := filepath.Join(dir, "child-started")
	markerPath := filepath.Join(dir, "child-finished")
	script := "(printf started > " + shellQuote(startedPath) + "; sleep 0.2; printf alive > " + shellQuote(markerPath) + ") &\nwait\n"
	adapter := &CommandAdapter{
		Command:        script,
		WorkingDir:     dir,
		CommandTimeout: 100 * time.Millisecond,
	}

	_, err := adapter.Fix("instruction", agentreport.View{}, AdapterMetadata{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Fix() error = %v, want timeout error", err)
	}
	if _, err := os.Stat(startedPath); err != nil {
		t.Fatalf("child start marker error = %v, want child process to have started", err)
	}

	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("child process marker error = %v, want child process killed", err)
	}
}

func TestCommandCandidateRunsLifecycleAndPollsHealth(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "lifecycle.log")
	command := func(phase string) string {
		return "printf '" + phase + " ' >> " + shellQuote(logPath)
	}
	var requests atomic.Int32
	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	candidate := &CommandCandidate{
		WorkingDir:     dir,
		StopCommand:    command("stop"),
		BuildCommand:   command("build"),
		StartCommand:   command("start"),
		HealthURL:      "http://candidate.test/health",
		HealthTimeout:  time.Second,
		HealthInterval: time.Millisecond,
		HTTPClient:     &http.Client{Transport: transport},
	}
	if err := candidate.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := candidate.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if err := candidate.Build(); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := candidate.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := candidate.WaitHealthy(); err != nil {
		t.Fatalf("WaitHealthy() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	var contents []byte
	var err error
	for time.Now().Before(deadline) {
		contents, err = os.ReadFile(logPath)
		if err == nil && string(contents) == "stop build start " {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got, want := string(contents), "stop build start "; got != want {
		t.Fatalf("lifecycle log = %q, want %q", got, want)
	}
	if requests.Load() < 2 {
		t.Fatalf("health requests = %d, want at least 2", requests.Load())
	}
}

func TestCommandCandidateHealthTimeoutIncludesLastFailure(t *testing.T) {
	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	candidate := &CommandCandidate{
		HealthURL:      "http://candidate.test/health",
		HealthTimeout:  10 * time.Millisecond,
		HealthInterval: time.Millisecond,
		HTTPClient:     &http.Client{Transport: transport},
	}
	err := candidate.WaitHealthy()
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("WaitHealthy() error = %v, want status and timeout", err)
	}
}

func TestCommandCandidateReportsImmediateStartFailure(t *testing.T) {
	candidate := &CommandCandidate{StartCommand: "exit 1"}
	if err := candidate.Start(); err == nil || !strings.Contains(err.Error(), "start command") {
		t.Fatalf("Start() error = %v, want start-command error", err)
	}
}

func TestCommandCandidateHookTimeoutIsReported(t *testing.T) {
	candidate := &CommandCandidate{
		BuildCommand:   "sleep 1",
		CommandTimeout: 20 * time.Millisecond,
	}

	err := candidate.Build()
	if err == nil || !strings.Contains(err.Error(), "build command") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Build() error = %v, want named timeout error", err)
	}
}

func writeExecutable(t *testing.T, dir string, name string, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
