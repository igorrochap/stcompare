package bench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
)

const (
	defaultHealthTimeout  = 30 * time.Second
	defaultHealthInterval = 100 * time.Millisecond
	defaultCommandTimeout = 30 * time.Minute
	startFailureGrace     = 10 * time.Millisecond
)

type scorecardBuildInput struct {
	ComparisonPath string
	RecordPath     string
	OutputPath     string
}

type scorecardBuilder interface {
	Build(scorecardBuildInput) error
}

type commandScorecardBuilder struct {
	Binary     string
	ConfigPath string
	WorkingDir string
	Env        []string
	Stdout     io.Writer
}

var _ scorecardBuilder = (*commandScorecardBuilder)(nil)

func newCommandScorecardBuilder(binary string, configPath string) *commandScorecardBuilder {
	return &commandScorecardBuilder{Binary: binary, ConfigPath: configPath}
}

func (builder *commandScorecardBuilder) Build(input scorecardBuildInput) error {
	if strings.TrimSpace(builder.Binary) == "" {
		return errors.New("stcompare binary is required")
	}

	args := make([]string, 0, 10)
	if builder.ConfigPath != "" {
		args = append(args, "--config", builder.ConfigPath)
	}
	args = append(
		args,
		"scorecard", "build",
		"--comparison", input.ComparisonPath,
		"--record", input.RecordPath,
		"--out", input.OutputPath,
	)
	stdout, stderr, err := runStcompare(builder.Binary, builder.WorkingDir, builder.Env, args, nil)
	if err != nil {
		return fmt.Errorf("run stcompare scorecard build: %w%s", err, formatCommandStderr(stderr))
	}
	if builder.Stdout != nil {
		if _, err := builder.Stdout.Write(stdout); err != nil {
			return fmt.Errorf("write stcompare scorecard output: %w", err)
		}
	}
	return nil
}

// CommandComparator invokes the stcompare CLI and consumes its agent contract.
type CommandComparator struct {
	Binary     string
	ConfigPath string
	BaseURL    string
	WorkingDir string
	Stderr     io.Writer
	Env        []string
}

var _ Comparator = (*CommandComparator)(nil)

// NewCommandComparator creates a comparator backed by an stcompare executable.
func NewCommandComparator(binary string, configPath string, baseURL string) *CommandComparator {
	return &CommandComparator{
		Binary:     binary,
		ConfigPath: configPath,
		BaseURL:    baseURL,
	}
}

// Compare runs campaign compare and parses its compact agent view.
func (comparator *CommandComparator) Compare(config Config) (agentreport.View, int, error) {
	if strings.TrimSpace(comparator.Binary) == "" {
		return agentreport.View{}, 0, errors.New("stcompare binary is required")
	}
	if strings.TrimSpace(config.Candidate) == "" {
		return agentreport.View{}, 0, errors.New("candidate campaign is required")
	}

	args := make([]string, 0, 10)
	if comparator.ConfigPath != "" {
		args = append(args, "--config", comparator.ConfigPath)
	}
	args = append(args, "campaign", "compare", config.Candidate, "--format", "agent")
	if comparator.BaseURL != "" {
		args = append(args, "--base-url", comparator.BaseURL)
	}

	stdout, stderr, runErr := runStcompare(
		comparator.Binary,
		comparator.WorkingDir,
		comparator.Env,
		args,
		comparator.Stderr,
	)
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return agentreport.View{}, 0, fmt.Errorf("run stcompare: %w", runErr)
		}
		exitCode = exitErr.ExitCode()
	}

	var view agentreport.View
	if err := decodeJSON(stdout, &view); err != nil {
		return agentreport.View{}, exitCode, fmt.Errorf(
			"decode stcompare agent view: %w%s",
			err,
			formatCommandStderr(stderr),
		)
	}

	switch exitCode {
	case agentreport.ExitCodeConverged,
		agentreport.ExitCodeToolError,
		agentreport.ExitCodeNotConverged:
		return view, exitCode, nil
	default:
		return agentreport.View{}, exitCode, fmt.Errorf(
			"stcompare exited with unexpected code %d%s",
			exitCode,
			formatCommandStderr(stderr),
		)
	}
}

func runStcompare(
	binary string,
	workingDir string,
	env []string,
	args []string,
	stderrWriter io.Writer,
) ([]byte, []byte, error) {
	command := exec.Command(binary, args...)
	command.Dir = workingDir
	command.Env = append(os.Environ(), env...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if stderrWriter != nil {
		command.Stderr = io.MultiWriter(&stderr, stderrWriter)
	}
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// AdapterRequest is the JSON request sent to an external adapter.
type AdapterRequest struct {
	AdapterMetadata
	Instruction  string           `json:"instruction"`
	View         agentreport.View `json:"view"`
	ReuseProcess bool             `json:"reuse_process,omitempty"`
}

// AdapterPreflightRequest is the no-op request sent before the first comparison.
type AdapterPreflightRequest struct {
	AdapterMetadata
	Preflight    bool `json:"preflight"`
	ReuseProcess bool `json:"reuse_process,omitempty"`
}

type adapterRequest interface {
	isAdapterRequest()
}

func (AdapterRequest) isAdapterRequest() {}

func (AdapterPreflightRequest) isAdapterRequest() {}

// AdapterResponse is the JSON result expected from an external adapter.
type AdapterResponse struct {
	Tokens       *benchrecord.TokenUsage `json:"tokens"`
	Response     string                  `json:"response"`
	Status       string                  `json:"status"`
	Message      string                  `json:"message"`
	ReuseProcess bool                    `json:"reuse_process"`
	Temperature  *float64                `json:"temperature,omitempty"`
}

// AdapterResult contains the result returned to the benchmark runner.
type AdapterResult struct {
	Tokens       *benchrecord.TokenUsage
	Response     string
	ReuseProcess bool
	Temperature  *float64
}

// CommandAdapter invokes a language-agnostic adapter process.
type CommandAdapter struct {
	Command        string
	WorkingDir     string
	CommandTimeout time.Duration
	Stderr         io.Writer
	Env            []string
	// ReuseProcess enables the negotiated line-delimited adapter protocol.
	// Adapters that do not advertise support during preflight use cold calls.
	ReuseProcess bool

	effectiveTemperature *float64

	reuse adapterReuseState
}

type adapterReuseState struct {
	process   *reusableAdapterProcess
	active    bool
	wasActive bool
}

var _ Adapter = (*CommandAdapter)(nil)

// NewCommandAdapter creates an adapter process configured for a candidate directory.
func NewCommandAdapter(command string, workingDir string) *CommandAdapter {
	return &CommandAdapter{
		Command:    command,
		WorkingDir: workingDir,
	}
}

// Preflight verifies that the adapter command accepts a no-op request.
func (adapter *CommandAdapter) Preflight(metadata AdapterMetadata) error {
	if adapter.ReuseProcess {
		result, err := adapter.runPersistentRequest(AdapterPreflightRequest{
			AdapterMetadata: metadata,
			Preflight:       true,
			ReuseProcess:    true,
		})
		if err != nil {
			return err
		}
		adapter.effectiveTemperature = result.Temperature
		if result.ReuseProcess && adapter.reuse.process != nil && adapter.reuse.process.running(adapterReuseProbe) {
			adapter.reuse.active = true
			adapter.reuse.wasActive = true
			return nil
		}
		// A stateless adapter may complete preflight successfully and exit.
		// That is the compatibility path: subsequent fixes use cold calls.
		return adapter.Close()
	}
	result, err := adapter.runRequest(AdapterPreflightRequest{
		AdapterMetadata: metadata,
		Preflight:       true,
	})
	if result != nil {
		adapter.effectiveTemperature = result.Temperature
	}
	return err
}

// EffectiveTemperature returns the temperature reported by the adapter during
// preflight, if one was provided.
func (adapter *CommandAdapter) EffectiveTemperature() *float64 {
	return adapter.effectiveTemperature
}

// Fix sends execution metadata, the rendered instruction, and compact view to the adapter.
func (adapter *CommandAdapter) Fix(
	instruction string,
	view agentreport.View,
	metadata AdapterMetadata,
) (*AdapterResult, error) {
	if adapter.reuse.active {
		return adapter.runPersistentRequest(AdapterRequest{
			AdapterMetadata: metadata,
			Instruction:     instruction,
			View:            view,
			ReuseProcess:    true,
		})
	}
	return adapter.runRequest(AdapterRequest{
		AdapterMetadata: metadata,
		Instruction:     instruction,
		View:            view,
	})
}

// ProcessReuseActive reports whether this adapter negotiated a reusable
// process during preflight. It remains true if that process later fails so the
// benchmark record discloses the mode that was actually used.
func (adapter *CommandAdapter) ProcessReuseActive() bool {
	return adapter.reuse.wasActive
}

// Close terminates a negotiated adapter process. It is safe to call more than
// once and is used by bench.Run on every terminal path.
func (adapter *CommandAdapter) Close() error {
	process := adapter.reuse.process
	adapter.reuse.process = nil
	adapter.reuse.active = false
	if process == nil {
		return nil
	}
	return process.close()
}

func (adapter *CommandAdapter) runRequest(request adapterRequest) (*AdapterResult, error) {
	input, err := adapter.encodeRequest(request)
	if err != nil {
		return nil, err
	}

	timeout := adapter.CommandTimeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command := exec.CommandContext(ctx, "sh", "-c", adapter.Command)
	configureProcessGroup(command)
	command.Cancel = func() error {
		return killProcessGroup(command.Process)
	}
	command.WaitDelay = time.Second
	command.Dir = adapter.WorkingDir
	command.Env = append(os.Environ(), adapter.Env...)
	command.Stdin = bytes.NewReader(input)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if adapter.Stderr != nil {
		command.Stderr = io.MultiWriter(&stderr, adapter.Stderr)
	}

	runErr := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf(
			"adapter command timed out after %s: %w%s",
			timeout,
			ctx.Err(),
			formatCommandStderr(stderr.Bytes()),
		)
	}
	result, responseErr := interpretAdapterResponse(stdout.Bytes())
	if responseErr != nil && result == nil {
		if runErr != nil {
			return nil, fmt.Errorf(
				"adapter command failed: %w: decode result: %v%s",
				runErr,
				responseErr,
				formatCommandStderr(stderr.Bytes()),
			)
		}
		return nil, fmt.Errorf("decode adapter result: %w", responseErr)
	}

	if runErr != nil {
		return result, fmt.Errorf(
			"adapter command failed: %w%s",
			runErr,
			formatCommandStderr(stderr.Bytes()),
		)
	}
	return result, responseErr
}

func (adapter *CommandAdapter) runPersistentRequest(request adapterRequest) (*AdapterResult, error) {
	input, err := adapter.encodeRequest(request)
	if err != nil {
		return nil, err
	}
	process, err := adapter.ensurePersistentProcess()
	if err != nil {
		return nil, err
	}
	timeout := adapter.CommandTimeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	output, err := process.request(ctx, input)
	if err != nil {
		adapter.reuse.process = nil
		adapter.reuse.active = false
		process.abort()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf(
				"adapter command timed out after %s: %w%s",
				timeout,
				ctx.Err(),
				formatCommandStderr(process.stderr.Bytes()),
			)
		}
		return nil, fmt.Errorf("reused adapter process failed: %w%s", err, formatCommandStderr(process.stderr.Bytes()))
	}

	result, responseErr := interpretAdapterResponse(output)
	if responseErr != nil && result == nil {
		return nil, fmt.Errorf("decode adapter result: %w%s", responseErr, formatCommandStderr(process.stderr.Bytes()))
	}
	return result, responseErr
}

func (adapter *CommandAdapter) encodeRequest(request adapterRequest) ([]byte, error) {
	if strings.TrimSpace(adapter.Command) == "" {
		return nil, errors.New("adapter command is required")
	}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode adapter request: %w", err)
	}
	return input, nil
}

func interpretAdapterResponse(output []byte) (*AdapterResult, error) {
	var response AdapterResponse
	if err := decodeJSON(output, &response); err != nil {
		return nil, err
	}
	result := &AdapterResult{
		Tokens:       response.Tokens,
		Response:     response.Response,
		ReuseProcess: response.ReuseProcess,
		Temperature:  response.Temperature,
	}
	switch response.Status {
	case "ok":
		return result, nil
	case "error":
		message := strings.TrimSpace(response.Message)
		if message == "" {
			message = "adapter returned status error"
		}
		return result, errors.New(message)
	default:
		return result, fmt.Errorf("adapter returned invalid status %q", response.Status)
	}
}

func (adapter *CommandAdapter) ensurePersistentProcess() (*reusableAdapterProcess, error) {
	if adapter.reuse.process != nil {
		return adapter.reuse.process, nil
	}

	command := exec.Command("sh", "-c", adapter.Command)
	configureProcessGroup(command)
	command.Dir = adapter.WorkingDir
	command.Env = append(os.Environ(), adapter.Env...)
	command.Env = append(command.Env, "STBENCH_REUSE_PROCESS=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open adapter stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open adapter stdout: %w", err)
	}
	stderr := &synchronizedBuffer{}
	if adapter.Stderr != nil {
		command.Stderr = io.MultiWriter(stderr, adapter.Stderr)
	} else {
		command.Stderr = stderr
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start reusable adapter: %w", err)
	}
	process := &reusableAdapterProcess{
		command: command,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		stderr:  stderr,
		done:    make(chan struct{}),
	}
	go func() {
		process.waitErr = command.Wait()
		close(process.done)
	}()
	adapter.reuse.process = process
	return process, nil
}

const (
	adapterCloseGrace = time.Second
	adapterReuseProbe = 10 * time.Millisecond
)

type reusableAdapterProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  *synchronizedBuffer
	done    chan struct{}
	waitErr error
}

type adapterReadResult struct {
	line []byte
	err  error
}

func (process *reusableAdapterProcess) request(ctx context.Context, input []byte) ([]byte, error) {
	// Cancellation is followed by abort, which closes stdin and kills the
	// process group so these I/O goroutines can finish. Buffered result channels
	// let them report completion even while the caller is tearing the process
	// down.
	payload := append(append([]byte(nil), input...), '\n')
	writeResult := make(chan error, 1)
	go func() {
		_, err := process.stdin.Write(payload)
		writeResult <- err
	}()
	select {
	case err := <-writeResult:
		if err != nil {
			return nil, fmt.Errorf("write adapter request: %w", err)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-process.done:
		return nil, fmt.Errorf("adapter process exited: %w", process.waitError())
	}

	readResult := make(chan adapterReadResult, 1)
	go func() {
		line, err := process.stdout.ReadBytes('\n')
		readResult <- adapterReadResult{line: line, err: err}
	}()
	select {
	case result := <-readResult:
		if result.err != nil {
			return nil, fmt.Errorf("read adapter response: %w", result.err)
		}
		return bytes.TrimSpace(result.line), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-process.done:
		return nil, fmt.Errorf("adapter process exited: %w", process.waitError())
	}
}

func (process *reusableAdapterProcess) running(probe time.Duration) bool {
	select {
	case <-process.done:
		return false
	default:
	}
	timer := time.NewTimer(probe)
	defer timer.Stop()
	select {
	case <-process.done:
		return false
	case <-timer.C:
		return true
	}
}

func (process *reusableAdapterProcess) abort() {
	_ = killProcessGroup(process.command.Process)
	_ = process.stdin.Close()
	<-process.done
}

func (process *reusableAdapterProcess) close() error {
	_ = process.stdin.Close()
	select {
	case <-process.done:
	case <-time.After(adapterCloseGrace):
		_ = killProcessGroup(process.command.Process)
		<-process.done
	}
	return process.waitError()
}

func (process *reusableAdapterProcess) waitError() error {
	return process.waitErr
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(value)
}

func (buffer *synchronizedBuffer) Bytes() []byte {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

type writerFunc func([]byte) (int, error)

func (write writerFunc) Write(value []byte) (int, error) {
	return write(value)
}

// CommandCandidate manages a candidate with configured shell commands.
type CommandCandidate struct {
	WorkingDir string

	StopCommand  string
	ResetCommand string
	BuildCommand string
	StartCommand string

	HealthURL      string
	HealthTimeout  time.Duration
	HealthInterval time.Duration
	HTTPClient     *http.Client

	CommandTimeout time.Duration
	Output         io.Writer
	ErrorOutput    io.Writer

	outputMu    sync.Mutex
	started     *exec.Cmd
	startedDone chan error
	startedEver bool
}

var _ Candidate = (*CommandCandidate)(nil)

// NewCommandCandidate creates a command-backed candidate lifecycle.
func NewCommandCandidate(workingDir string) *CommandCandidate {
	return &CommandCandidate{WorkingDir: workingDir}
}

// Stop runs the configured stop hook and terminates a process started by Start.
func (candidate *CommandCandidate) Stop() error {
	hookErr := candidate.runHook("stop", candidate.StopCommand)
	candidate.terminateStartedProcess()
	candidate.startedEver = false
	return hookErr
}

// Reset runs the optional reset hook.
func (candidate *CommandCandidate) Reset() error {
	return candidate.runHook("reset", candidate.ResetCommand)
}

// Build runs the configured build hook.
func (candidate *CommandCandidate) Build() error {
	return candidate.runHook("build", candidate.BuildCommand)
}

// Start starts the configured candidate process without waiting for it to exit.
func (candidate *CommandCandidate) Start() error {
	if strings.TrimSpace(candidate.StartCommand) == "" {
		return nil
	}

	command := exec.Command("sh", "-c", candidate.StartCommand)
	candidate.configureCommand(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	candidate.started = command
	candidate.startedEver = true
	candidate.startedDone = make(chan error, 1)
	done := candidate.startedDone
	go func() {
		done <- command.Wait()
	}()
	select {
	case waitErr := <-done:
		candidate.started = nil
		candidate.startedDone = nil
		if waitErr != nil {
			return fmt.Errorf("start command: %w", waitErr)
		}
	case <-time.After(startFailureGrace):
	}
	return nil
}

// WaitHealthy polls the configured HTTP health endpoint until it returns 2xx.
func (candidate *CommandCandidate) WaitHealthy() error {
	if strings.TrimSpace(candidate.HealthURL) == "" {
		return errors.New("health URL is required")
	}
	parsedURL, err := url.Parse(candidate.HealthURL)
	if err != nil || parsedURL.Host == "" ||
		(!strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https")) {
		return fmt.Errorf("health URL must be an absolute HTTP(S) URL")
	}

	timeout := candidate.HealthTimeout
	if timeout <= 0 {
		timeout = defaultHealthTimeout
	}
	interval := candidate.HealthInterval
	if interval <= 0 {
		interval = defaultHealthInterval
	}
	client := candidate.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var lastErr error
	for {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, candidate.HealthURL, nil)
		if requestErr != nil {
			return fmt.Errorf("create health request: %w", requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				return nil
			}
			status := response.Status
			if status == "" {
				status = fmt.Sprintf("%d", response.StatusCode)
			}
			lastErr = fmt.Errorf("health endpoint returned %s", status)
		} else {
			lastErr = requestErr
		}

		if ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}

	if lastErr == nil {
		lastErr = ctx.Err()
	}
	return fmt.Errorf("health check %q timed out: %w", candidate.HealthURL, lastErr)
}

func (candidate *CommandCandidate) runHook(name string, hook string) error {
	if strings.TrimSpace(hook) == "" {
		return nil
	}

	timeout := candidate.CommandTimeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command := exec.CommandContext(ctx, "sh", "-c", hook)
	configureProcessGroup(command)
	command.Cancel = func() error {
		return killProcessGroup(command.Process)
	}
	command.WaitDelay = time.Second
	candidate.configureCommand(command)
	runErr := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s command timed out after %s: %w", name, timeout, ctx.Err())
	}
	if runErr != nil {
		return fmt.Errorf("%s command: %w", name, runErr)
	}
	return nil
}

func (candidate *CommandCandidate) configureCommand(command *exec.Cmd) {
	configureProcessGroup(command)
	command.Dir = candidate.WorkingDir
	command.Env = os.Environ()
	if candidate.Output != nil {
		command.Stdout = writerFunc(candidate.writeOutput)
	}
	if candidate.ErrorOutput != nil {
		command.Stderr = writerFunc(candidate.writeErrorOutput)
	}
}

func (candidate *CommandCandidate) writeOutput(value []byte) (int, error) {
	candidate.outputMu.Lock()
	defer candidate.outputMu.Unlock()

	return candidate.Output.Write(value)
}

func (candidate *CommandCandidate) writeErrorOutput(value []byte) (int, error) {
	candidate.outputMu.Lock()
	defer candidate.outputMu.Unlock()

	return candidate.ErrorOutput.Write(value)
}

func (candidate *CommandCandidate) terminateStartedProcess() {
	if candidate.started == nil || candidate.started.Process == nil {
		return
	}
	_ = killProcessGroup(candidate.started.Process)
	if candidate.startedDone != nil {
		<-candidate.startedDone
	}
	candidate.started = nil
	candidate.startedDone = nil
}

func decodeJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func formatCommandStderr(stderr []byte) string {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return ""
	}
	return ": " + message
}
