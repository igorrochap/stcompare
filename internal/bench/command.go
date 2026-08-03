package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"stcompare/agentreport"
	"stcompare/benchrecord"
	"stcompare/internal/config"
)

const defaultRecordPath = "benchmark-record.json"

// ExitCodeError carries the process exit code selected by stbench.
type ExitCodeError struct {
	// Code is the process exit code selected by the command.
	Code int
	// Err is the underlying command failure.
	Err error
}

func (err *ExitCodeError) Error() string {
	return err.Err.Error()
}

func (err *ExitCodeError) Unwrap() error {
	return err.Err
}

// NewRootCommand creates the stbench command tree.
func NewRootCommand() *cobra.Command {
	options := rootCommandOptions{}
	root := &cobra.Command{
		Use:          "stbench",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&options.configPath, "config", config.DefaultFilename, "stcompare configuration path")
	root.AddCommand(newInitCommand())
	root.AddCommand(newRunCommand(&options))
	return root
}

type rootCommandOptions struct {
	configPath string
}

type runCommandOptions struct {
	candidate       string
	agent           string
	model           string
	hardware        string
	adapter         string
	adapterTimeout  string
	candidateDir    string
	stcompareBinary string
	recordPath      string
	baseURL         string
	stop            string
	reset           string
	build           string
	start           string
	commandTimeout  string
	healthURL       string
	healthTimeout   string
	healthInterval  string
	maxIterations   int
	stallWindow     int
	promptID        string
	promptVersion   string
}

func newRunCommand(rootOptions *rootCommandOptions) *cobra.Command {
	options := runCommandOptions{}
	command := &cobra.Command{
		Use:  "run [candidate]",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if cmd.Flags().Changed("candidate") && options.candidate != args[0] {
					return errors.New("candidate is specified both as an argument and a flag")
				}
				options.candidate = args[0]
			}
			return runCommand(cmd, rootOptions.configPath, options)
		},
	}

	flags := command.Flags()
	flags.StringVar(&options.candidate, "candidate", "", "candidate campaign name")
	flags.StringVar(&options.agent, "agent", "", "agent metadata recorded and passed to the adapter")
	flags.StringVar(&options.model, "model", "", "model metadata recorded and passed to the adapter")
	flags.StringVar(&options.hardware, "hardware", "", "hardware metadata recorded and passed to the adapter")
	flags.StringVar(&options.adapter, "adapter", "", "adapter command")
	flags.StringVar(&options.adapter, "adapter-command", "", "alias for --adapter")
	flags.StringVar(&options.adapterTimeout, "adapter-timeout", "", "adapter command timeout")
	flags.StringVar(&options.candidateDir, "candidate-dir", "", "candidate source directory")
	flags.StringVar(&options.candidateDir, "source-dir", "", "alias for --candidate-dir")
	flags.StringVar(&options.stcompareBinary, "stcompare-binary", "", "stcompare executable")
	flags.StringVar(&options.stcompareBinary, "stcompare", "", "alias for --stcompare-binary")
	flags.StringVar(&options.recordPath, "record", "", "benchmark record output path")
	flags.StringVar(&options.recordPath, "record-path", "", "alias for --record")
	flags.StringVar(&options.baseURL, "base-url", "", "candidate base URL override")
	flags.StringVar(&options.stop, "stop", "", "candidate stop command")
	flags.StringVar(&options.stop, "stop-command", "", "alias for --stop")
	flags.StringVar(&options.reset, "reset", "", "optional candidate reset command")
	flags.StringVar(&options.reset, "reset-command", "", "alias for --reset")
	flags.StringVar(&options.build, "build", "", "candidate build command")
	flags.StringVar(&options.build, "build-command", "", "alias for --build")
	flags.StringVar(&options.start, "start", "", "candidate start command")
	flags.StringVar(&options.start, "start-command", "", "alias for --start")
	flags.StringVar(&options.commandTimeout, "command-timeout", "", "candidate lifecycle command timeout")
	flags.StringVar(&options.healthURL, "health-url", "", "candidate health-check URL")
	flags.StringVar(&options.healthTimeout, "health-timeout", "", "health-check timeout")
	flags.StringVar(&options.healthInterval, "health-interval", "", "health-check polling interval")
	flags.IntVar(&options.maxIterations, "max-iterations", 0, "maximum benchmark iterations")
	flags.IntVar(&options.stallWindow, "stall-window", 0, "non-improving transitions before stalling")
	flags.StringVar(&options.promptID, "prompt-id", "", "task prompt identity")
	flags.StringVar(&options.promptVersion, "prompt-version", "", "task prompt version")

	return command
}

func runCommand(command *cobra.Command, configPath string, options runCommandOptions) error {
	effective, err := config.Load(configPath)
	if err != nil {
		return err
	}
	applyRunOverrides(command, &effective, &options)
	if err := effective.Validate(); err != nil {
		return err
	}

	settings := defaultRunSettings(effective.Stbench)
	applyRunSettings(&settings, options, command)
	if err := validateRunSettings(effective, settings); err != nil {
		return err
	}

	workingDir, err := filepath.Abs(settings.candidateDir)
	if err != nil {
		return fmt.Errorf("resolve candidate directory: %w", err)
	}
	if info, statErr := os.Stat(workingDir); statErr != nil {
		return fmt.Errorf("inspect candidate directory: %w", statErr)
	} else if !info.IsDir() {
		return fmt.Errorf("candidate directory %s is not a directory", workingDir)
	}

	healthTimeout, err := parseDuration("health timeout", settings.healthTimeout)
	if err != nil {
		return err
	}
	healthInterval, err := parseDuration("health interval", settings.healthInterval)
	if err != nil {
		return err
	}
	adapterTimeout, err := parseDuration("adapter timeout", settings.adapterTimeout)
	if err != nil {
		return err
	}
	commandTimeout, err := parseDuration("command timeout", settings.commandTimeout)
	if err != nil {
		return err
	}

	baselineName := findBaselineName(effective)
	benchConfig := Config{
		AdapterMetadata: AdapterMetadata{
			Agent:    settings.agent,
			Model:    settings.model,
			Hardware: settings.hardware,
		},
		Candidate:     settings.candidate,
		Baseline:      baselineName,
		Prompt:        benchrecord.PromptIdentity{ID: settings.promptID, Version: settings.promptVersion},
		MaxIterations: settings.maxIterations,
		StallWindow:   settings.stallWindow,
		BaselineExists: func() bool {
			_, statErr := os.Stat(filepath.Join(effective.ReportsDir, baselineName, "campaign.har.json"))
			return statErr == nil
		},
	}

	comparator := NewCommandComparator(settings.stcompareBinary, configPath, effective.BaseURL)
	comparator.Stderr = command.ErrOrStderr()
	candidate := NewCommandCandidate(workingDir)
	candidate.StopCommand = settings.stop
	candidate.ResetCommand = settings.reset
	candidate.BuildCommand = settings.build
	candidate.StartCommand = settings.start
	candidate.CommandTimeout = commandTimeout
	candidate.HealthURL = settings.healthURL
	candidate.HealthTimeout = healthTimeout
	candidate.HealthInterval = healthInterval
	candidate.ErrorOutput = command.ErrOrStderr()
	adapter := NewCommandAdapter(settings.adapter, workingDir)
	adapter.CommandTimeout = adapterTimeout
	adapter.Stderr = command.ErrOrStderr()

	defer func() {
		if !candidate.startedEver {
			return
		}
		if stopErr := candidate.Stop(); stopErr != nil {
			fmt.Fprintf(command.ErrOrStderr(), "stop candidate: %v\n", stopErr)
		}
	}()

	record, runErr := Run(benchConfig, Dependencies{
		Comparator: comparator,
		Candidate:  candidate,
		Adapter:    adapter,
	})
	if err := writeRecord(settings.recordPath, record); err != nil {
		if runErr != nil {
			return fmt.Errorf("%v; write benchmark record: %w", runErr, err)
		}
		return err
	}
	fmt.Fprintf(command.OutOrStdout(), "wrote %s\n", settings.recordPath)

	if runErr != nil {
		return &ExitCodeError{Code: exitCodeForState(record.TerminalState), Err: runErr}
	}
	if code := exitCodeForState(record.TerminalState); code != 0 {
		return &ExitCodeError{
			Code: code,
			Err:  fmt.Errorf("benchmark ended with terminal state %q", record.TerminalState),
		}
	}
	return nil
}

type runSettings struct {
	candidate       string
	agent           string
	model           string
	hardware        string
	adapter         string
	adapterTimeout  string
	candidateDir    string
	stcompareBinary string
	recordPath      string
	stop            string
	reset           string
	build           string
	start           string
	commandTimeout  string
	healthURL       string
	healthTimeout   string
	healthInterval  string
	maxIterations   int
	stallWindow     int
	promptID        string
	promptVersion   string
}

func defaultRunSettings(source *config.StbenchConfig) runSettings {
	settings := runSettings{
		candidateDir:    ".",
		stcompareBinary: "stcompare",
		recordPath:      defaultRecordPath,
	}
	if source == nil {
		return settings
	}
	settings.candidate = source.Candidate
	settings.agent = source.Agent
	settings.model = source.Model
	settings.hardware = source.Hardware
	settings.adapter = source.Adapter
	settings.adapterTimeout = source.AdapterTimeout
	if source.CandidateDir != "" {
		settings.candidateDir = source.CandidateDir
	}
	if source.StcompareBinary != "" {
		settings.stcompareBinary = source.StcompareBinary
	}
	if source.RecordPath != "" {
		settings.recordPath = source.RecordPath
	}
	settings.stop = source.Lifecycle.Stop
	settings.reset = source.Lifecycle.Reset
	settings.build = source.Lifecycle.Build
	settings.start = source.Lifecycle.Start
	settings.commandTimeout = source.Lifecycle.CommandTimeout
	settings.healthURL = source.Lifecycle.HealthURL
	settings.healthTimeout = source.Lifecycle.HealthTimeout
	settings.healthInterval = source.Lifecycle.HealthInterval
	settings.maxIterations = source.MaxIterations
	settings.stallWindow = source.StallWindow
	settings.promptID = source.Prompt.ID
	settings.promptVersion = source.Prompt.Version
	return settings
}

func applyRunSettings(settings *runSettings, options runCommandOptions, command *cobra.Command) {
	if command.Flags().Changed("candidate") || options.candidate != "" {
		settings.candidate = options.candidate
	}
	if command.Flags().Changed("agent") {
		settings.agent = options.agent
	}
	if command.Flags().Changed("model") {
		settings.model = options.model
	}
	if command.Flags().Changed("hardware") {
		settings.hardware = options.hardware
	}
	if command.Flags().Changed("adapter") || command.Flags().Changed("adapter-command") {
		settings.adapter = options.adapter
	}
	if command.Flags().Changed("adapter-timeout") {
		settings.adapterTimeout = options.adapterTimeout
	}
	if command.Flags().Changed("candidate-dir") || command.Flags().Changed("source-dir") {
		settings.candidateDir = options.candidateDir
	}
	if command.Flags().Changed("stcompare-binary") || command.Flags().Changed("stcompare") {
		settings.stcompareBinary = options.stcompareBinary
	}
	if command.Flags().Changed("record") || command.Flags().Changed("record-path") {
		settings.recordPath = options.recordPath
	}
	if command.Flags().Changed("stop") || command.Flags().Changed("stop-command") {
		settings.stop = options.stop
	}
	if command.Flags().Changed("reset") || command.Flags().Changed("reset-command") {
		settings.reset = options.reset
	}
	if command.Flags().Changed("build") || command.Flags().Changed("build-command") {
		settings.build = options.build
	}
	if command.Flags().Changed("start") || command.Flags().Changed("start-command") {
		settings.start = options.start
	}
	if command.Flags().Changed("command-timeout") {
		settings.commandTimeout = options.commandTimeout
	}
	if command.Flags().Changed("health-url") {
		settings.healthURL = options.healthURL
	}
	if command.Flags().Changed("health-timeout") {
		settings.healthTimeout = options.healthTimeout
	}
	if command.Flags().Changed("health-interval") {
		settings.healthInterval = options.healthInterval
	}
	if command.Flags().Changed("max-iterations") {
		settings.maxIterations = options.maxIterations
	}
	if command.Flags().Changed("stall-window") {
		settings.stallWindow = options.stallWindow
	}
	if command.Flags().Changed("prompt-id") {
		settings.promptID = options.promptID
	}
	if command.Flags().Changed("prompt-version") {
		settings.promptVersion = options.promptVersion
	}
}

func applyRunOverrides(command *cobra.Command, effective *config.Config, options *runCommandOptions) {
	if command.Flags().Changed("base-url") {
		effective.BaseURL = options.baseURL
	}
}

func validateRunSettings(effective config.Config, settings runSettings) error {
	if strings.TrimSpace(settings.candidate) == "" {
		return errors.New("candidate is required")
	}
	campaign, ok := effective.Campaigns[settings.candidate]
	if !ok {
		return fmt.Errorf("campaign %q is not configured", settings.candidate)
	}
	if campaign.Kind != "candidate" {
		return fmt.Errorf("campaign %q has kind %q: stbench requires a candidate campaign", settings.candidate, campaign.Kind)
	}
	if strings.TrimSpace(settings.adapter) == "" {
		return errors.New("adapter command is required")
	}
	if strings.TrimSpace(settings.stop) == "" {
		return errors.New("stop command is required")
	}
	if strings.TrimSpace(settings.build) == "" {
		return errors.New("build command is required")
	}
	if strings.TrimSpace(settings.start) == "" {
		return errors.New("start command is required")
	}
	if strings.TrimSpace(settings.healthURL) == "" {
		return errors.New("health URL is required")
	}
	if strings.TrimSpace(settings.recordPath) == "" {
		return errors.New("record path is required")
	}
	if settings.maxIterations < 0 {
		return errors.New("max iterations must not be negative")
	}
	if settings.stallWindow < 0 {
		return errors.New("stall window must not be negative")
	}
	return nil
}

func parseDuration(name string, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q is invalid: %w", name, value, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return duration, nil
}

func findBaselineName(effective config.Config) string {
	for name, campaign := range effective.Campaigns {
		if campaign.Kind == "baseline" {
			return name
		}
	}
	return ""
}

func writeRecord(path string, record benchrecord.Record) error {
	contents, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode benchmark record: %w", err)
	}
	contents = append(contents, '\n')
	if parent := filepath.Dir(path); parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create record directory: %w", err)
		}
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write benchmark record: %w", err)
	}
	return nil
}

func exitCodeForState(state benchrecord.TerminalState) int {
	switch state {
	case benchrecord.TerminalStateConverged:
		return 0
	case benchrecord.TerminalStateStalled, benchrecord.TerminalStateMaxIterations:
		return agentreport.ExitCodeNotConverged
	default:
		return agentreport.ExitCodeToolError
	}
}
