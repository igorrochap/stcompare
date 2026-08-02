package bench

import (
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
	"time"

	"stcompare/agentreport"
	"stcompare/benchrecord"
)

const (
	defaultHealthTimeout  = 30 * time.Second
	defaultHealthInterval = 100 * time.Millisecond
	startFailureGrace     = 10 * time.Millisecond
)

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

	command := exec.Command(comparator.Binary, args...)
	command.Dir = comparator.WorkingDir
	command.Env = append(os.Environ(), comparator.Env...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if comparator.Stderr != nil {
		command.Stderr = io.MultiWriter(&stderr, comparator.Stderr)
	}

	runErr := command.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return agentreport.View{}, 0, fmt.Errorf("run stcompare: %w", runErr)
		}
		exitCode = exitErr.ExitCode()
	}

	var view agentreport.View
	if err := decodeJSON(stdout.Bytes(), &view); err != nil {
		return agentreport.View{}, exitCode, fmt.Errorf(
			"decode stcompare agent view: %w%s",
			err,
			formatCommandStderr(stderr.Bytes()),
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
			formatCommandStderr(stderr.Bytes()),
		)
	}
}

// AdapterRequest is the JSON request sent to an external adapter.
type AdapterRequest struct {
	Instruction string           `json:"instruction"`
	View        agentreport.View `json:"view"`
}

// AdapterResponse is the JSON result expected from an external adapter.
type AdapterResponse struct {
	Tokens   *benchrecord.TokenUsage `json:"tokens"`
	Response string                  `json:"response"`
	Status   string                  `json:"status"`
	Message  string                  `json:"message"`
}

// AdapterResult contains the result returned to the benchmark runner.
type AdapterResult struct {
	Tokens   *benchrecord.TokenUsage
	Response string
}

// CommandAdapter invokes a language-agnostic adapter process.
type CommandAdapter struct {
	Command    string
	WorkingDir string
	Stderr     io.Writer
	Env        []string
}

var _ Adapter = (*CommandAdapter)(nil)

// NewCommandAdapter creates an adapter process configured for a candidate directory.
func NewCommandAdapter(command string, workingDir string) *CommandAdapter {
	return &CommandAdapter{
		Command:    command,
		WorkingDir: workingDir,
	}
}

// Fix sends the rendered instruction and compact view to the adapter.
func (adapter *CommandAdapter) Fix(instruction string, view agentreport.View) (*AdapterResult, error) {
	if strings.TrimSpace(adapter.Command) == "" {
		return nil, errors.New("adapter command is required")
	}

	request := AdapterRequest{Instruction: instruction, View: view}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode adapter request: %w", err)
	}

	command := exec.Command("sh", "-c", adapter.Command)
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
	var response AdapterResponse
	if err := decodeJSON(stdout.Bytes(), &response); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf(
				"adapter command failed: %w: decode result: %v%s",
				runErr,
				err,
				formatCommandStderr(stderr.Bytes()),
			)
		}
		return nil, fmt.Errorf("decode adapter result: %w", err)
	}
	result := &AdapterResult{
		Tokens:   response.Tokens,
		Response: response.Response,
	}

	if runErr != nil {
		return result, fmt.Errorf(
			"adapter command failed: %w%s",
			runErr,
			formatCommandStderr(stderr.Bytes()),
		)
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
	if err := candidate.runHook("stop", candidate.StopCommand); err != nil {
		return err
	}
	candidate.terminateStartedProcess()
	candidate.startedEver = false
	return nil
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

	var command *exec.Cmd
	var cancel context.CancelFunc
	if candidate.CommandTimeout > 0 {
		var ctx context.Context
		ctx, cancel = context.WithTimeout(context.Background(), candidate.CommandTimeout)
		command = exec.CommandContext(ctx, "sh", "-c", hook)
	} else {
		command = exec.Command("sh", "-c", hook)
	}
	if cancel != nil {
		defer cancel()
	}
	candidate.configureCommand(command)
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s command: %w", name, err)
	}
	return nil
}

func (candidate *CommandCandidate) configureCommand(command *exec.Cmd) {
	command.Dir = candidate.WorkingDir
	command.Env = os.Environ()
	command.Stdout = candidate.Output
	command.Stderr = candidate.ErrorOutput
}

func (candidate *CommandCandidate) terminateStartedProcess() {
	if candidate.started == nil || candidate.started.Process == nil {
		return
	}
	_ = candidate.started.Process.Kill()
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
