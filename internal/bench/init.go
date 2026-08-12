package bench

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"stcompare/examples/stbench"
	"stcompare/internal/config"
)

var lifecycleScaffold = []struct {
	name     string
	contents string
}{
	{
		name: "stop.sh",
		contents: `#!/bin/sh
set -eu

# stop must no-op cleanly when nothing is running.
# Replace this no-op with a process-manager command that tolerates a missing process.
:
`,
	},
	{
		name: "reset.sh",
		contents: `#!/bin/sh
set -eu

# IMPORTANT: reset must not revert source changes.
# Clean per-iteration runtime state here, such as a database or generated files.
:
`,
	},
	{
		name: "build.sh",
		contents: `#!/bin/sh
set -eu

# Replace this no-op with the candidate's build or compile command.
:
`,
	},
	{
		name: "start.sh",
		contents: `#!/bin/sh
set -eu

# Replace this with the candidate's long-running server command.
# stbench starts this script asynchronously and polls the configured health URL.
exec tail -f /dev/null
`,
	},
}

const stbenchStateDirEnv = "STBENCH_STATE_DIR"

const managedStateGitignoreEntry = ".local/stbench/"

type initDependencies struct {
	isTerminal     func(int) bool
	selectAdapters func(*cobra.Command) ([]stbenchadapters.Role, error)
}

type adapterChoice struct {
	label string
	role  stbenchadapters.Role
}

type initScaffold struct {
	created         []string
	createdAdapters []string
}

type fileSnapshot struct {
	path     string
	contents []byte
	existed  bool
}

type checklistRunner func(
	input io.Reader,
	output io.Writer,
	choices []adapterChoice,
) ([]stbenchadapters.Role, error)

type fileDescriptor interface {
	Fd() uintptr
}

func newInitDependencies(isTerminal func(int) bool, runChecklist checklistRunner) initDependencies {
	return initDependencies{
		isTerminal:     isTerminal,
		selectAdapters: newAdapterSelector(runChecklist),
	}
}

func defaultInitDependencies() initDependencies {
	return newInitDependencies(term.IsTerminal, runHuhChecklist)
}

func runHuhChecklist(
	input io.Reader,
	output io.Writer,
	choices []adapterChoice,
) ([]stbenchadapters.Role, error) {
	options := make([]huh.Option[stbenchadapters.Role], 0, len(choices))
	for _, choice := range choices {
		options = append(options, huh.NewOption(choice.label, choice.role))
	}

	var selected []stbenchadapters.Role
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[stbenchadapters.Role]().
				Title("Select adapters to install").
				Options(options...).
				Value(&selected),
		),
	).WithInput(input).WithOutput(output).Run()
	if err != nil {
		return nil, err
	}
	return selected, nil
}

func newAdapterSelector(runChecklist checklistRunner) func(*cobra.Command) ([]stbenchadapters.Role, error) {
	return func(command *cobra.Command) ([]stbenchadapters.Role, error) {
		choices := []adapterChoice{
			{label: "cloud", role: stbenchadapters.RoleCloud},
			{label: "local", role: stbenchadapters.RoleLocal},
			{label: "coding-agent", role: stbenchadapters.RoleCodingAgent},
		}
		return runChecklist(command.InOrStdin(), command.OutOrStdout(), choices)
	}
}

func newInitCommand(deps initDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:  "init",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runInit(command, args, deps)
		},
	}
	command.Flags().String("adapters", "", "comma-separated list of adapters to install")
	return command
}

func runInit(command *cobra.Command, _ []string, deps initDependencies) error {
	adapterFiles, err := adapterFilesForInit(command, deps)
	if err != nil {
		return err
	}

	repositoryDir := "."
	harnessDir, err := managedHarnessDir(repositoryDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		return fmt.Errorf("create managed stbench directory: %w", err)
	}
	resyncProtocolFile, err := shouldResyncProtocol(harnessDir, adapterFiles)
	if err != nil {
		return err
	}

	scaffold, err := prepareInitScaffold(repositoryDir, harnessDir, adapterFiles)
	if err != nil {
		return err
	}

	configPath := filepath.Join(repositoryDir, config.DefaultFilename)
	configSnapshot, err := newFileSnapshot(configPath)
	if err != nil {
		removeCreatedFiles(scaffold.created)
		return err
	}
	configErr := appendStbenchConfig(configPath, stbenchConfigBlockForAdapters(harnessDir, adapterFiles))
	if configErr != nil && !errors.Is(configErr, errStbenchConfigPresent) {
		return rollbackInit(scaffold.created, configSnapshot, configErr)
	}

	if resyncProtocolFile {
		if err := resyncProtocol(harnessDir); err != nil {
			return rollbackInit(scaffold.created, configSnapshot, err)
		}
	}

	return writeInitConfirmation(
		command.OutOrStdout(),
		configPath,
		configErr,
		len(scaffold.createdAdapters) > 0,
	)
}

func prepareInitScaffold(
	repositoryDir string,
	harnessDir string,
	adapterFiles []stbenchadapters.AdapterFile,
) (initScaffold, error) {
	created, err := writeLifecycleScaffold(harnessDir)
	if err != nil {
		return initScaffold{}, err
	}
	createdAdapterFiles, err := writeAdapterScaffold(harnessDir, adapterFiles)
	if err != nil {
		removeCreatedFiles(created)
		return initScaffold{}, err
	}
	created = append(created, createdAdapterFiles...)
	if err := ensureManagedStateIgnored(repositoryDir); err != nil {
		removeCreatedFiles(created)
		return initScaffold{}, fmt.Errorf("update .gitignore: %w", err)
	}

	return initScaffold{
		created:         created,
		createdAdapters: createdAdapterFiles,
	}, nil
}

func writeInitConfirmation(
	output io.Writer,
	configPath string,
	configErr error,
	installedAdapters bool,
) error {
	switch {
	case errors.Is(configErr, errStbenchConfigPresent) && installedAdapters:
		if _, err := fmt.Fprintf(
			output,
			"Installed missing adapter files; %s already has matching adapter configuration\n",
			configPath,
		); err != nil {
			return fmt.Errorf("write init confirmation: %w", err)
		}
	case errors.Is(configErr, errStbenchConfigPresent):
		// stcompare config init is the authoritative writer of the stbench block.
		// When one is already present, keep the lifecycle scripts and leave the
		// block untouched instead of failing and rolling them back.
		if _, err := fmt.Fprintf(output, "%s already has an stbench block; kept the lifecycle scripts and left the block unchanged\n", configPath); err != nil {
			return fmt.Errorf("write init confirmation: %w", err)
		}
	default:
		if _, err := fmt.Fprintf(output, "Wrote stbench configuration to %s\n", configPath); err != nil {
			return fmt.Errorf("write init confirmation: %w", err)
		}
	}

	return nil
}

func adapterFilesForInit(command *cobra.Command, deps initDependencies) ([]stbenchadapters.AdapterFile, error) {
	if !command.Flags().Changed("adapters") {
		if !commandIsInteractive(command, deps.isTerminal) {
			return stbenchadapters.Files(), nil
		}

		roles, err := deps.selectAdapters(command)
		if err != nil {
			return nil, fmt.Errorf("select adapters: %w", err)
		}
		if len(roles) == 0 {
			return []stbenchadapters.AdapterFile{}, nil
		}
		return adapterFilesForSelectedRoles(rolesToSet(roles)), nil
	}

	selection, err := command.Flags().GetString("adapters")
	if err != nil {
		return nil, fmt.Errorf("read adapters selection: %w", err)
	}

	selectedRoles := make([]stbenchadapters.Role, 0, 3)
	for _, token := range strings.Split(selection, ",") {
		role := stbenchadapters.Role(strings.TrimSpace(token))
		if role == "" {
			continue
		}
		if !validAdapterRole(role) {
			return nil, fmt.Errorf("unknown adapter %q; valid adapters: cloud, local, coding-agent", role)
		}
		selectedRoles = append(selectedRoles, role)
	}

	return adapterFilesForSelectedRoles(rolesToSet(selectedRoles)), nil
}

func rolesToSet(roles []stbenchadapters.Role) map[stbenchadapters.Role]bool {
	roleSet := make(map[stbenchadapters.Role]bool, len(roles))
	for _, role := range roles {
		roleSet[role] = true
	}
	return roleSet
}

func adapterFilesForSelectedRoles(selectedRoles map[stbenchadapters.Role]bool) []stbenchadapters.AdapterFile {
	if len(selectedRoles) == 0 {
		return nil
	}

	selectedFiles := make([]stbenchadapters.AdapterFile, 0, len(selectedRoles)+1)
	for _, adapter := range stbenchadapters.Files() {
		if adapter.Role == "" || selectedRoles[adapter.Role] {
			selectedFiles = append(selectedFiles, adapter)
		}
	}
	return selectedFiles
}

func commandIsInteractive(command *cobra.Command, isTerminal func(int) bool) bool {
	stdin, ok := command.InOrStdin().(fileDescriptor)
	if !ok {
		return false
	}
	stdout, ok := command.OutOrStdout().(fileDescriptor)
	if !ok {
		return false
	}
	return isTerminal(int(stdin.Fd())) && isTerminal(int(stdout.Fd()))
}

func validAdapterRole(role stbenchadapters.Role) bool {
	switch role {
	case stbenchadapters.RoleCloud, stbenchadapters.RoleLocal, stbenchadapters.RoleCodingAgent:
		return true
	default:
		return false
	}
}

// errStbenchConfigPresent signals that stcompare.yaml already carries every
// selected adapter entry, so init leaves it unchanged rather than treating it
// as a failure.
var errStbenchConfigPresent = errors.New("stcompare.yaml already contains an stbench block")

func appendStbenchConfig(path string, block string) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s does not exist; run stcompare config init first", path)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	root, err := yamlMappingRoot(&document)
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var blockDocument yaml.Node
	if err := yaml.Unmarshal([]byte(block), &blockDocument); err != nil {
		return fmt.Errorf("decode generated stbench config: %w", err)
	}
	blockRoot, err := yamlMappingRoot(&blockDocument)
	if err != nil {
		return fmt.Errorf("decode generated stbench config: %w", err)
	}
	generatedStbench := yamlMappingValue(blockRoot, "stbench")
	if generatedStbench == nil {
		return errors.New("decode generated stbench config: missing stbench mapping")
	}

	existingStbench := yamlMappingValue(root, "stbench")
	if existingStbench == nil {
		root.Content = append(root.Content, blockRoot.Content...)
		return writeYAMLDocument(path, &document)
	}
	if existingStbench.Kind != yaml.MappingNode {
		return fmt.Errorf("decode %s: stbench must be a YAML mapping", path)
	}
	generatedAdapters := yamlMappingValue(generatedStbench, "adapters")
	if generatedAdapters == nil || generatedAdapters.Kind != yaml.MappingNode {
		return errors.New("decode generated stbench config: missing adapters mapping")
	}
	existingAdapters := yamlMappingValue(existingStbench, "adapters")
	missingEntries, err := missingYAMLMappingEntries(existingAdapters, generatedAdapters)
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if len(missingEntries) == 0 {
		return errStbenchConfigPresent
	}

	var updated []byte
	appender := yamlMappingAppender{
		contents: contents,
		mapping:  existingStbench,
		entries:  missingEntries,
	}
	if existingAdapters == nil {
		updated, err = appender.appendMapping(path, root)
	} else {
		appender.mapping = existingAdapters
		updated, err = appender.appendEntries()
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

type yamlMappingEntry struct {
	key   string
	value string
}

type yamlMappingAppender struct {
	contents []byte
	mapping  *yaml.Node
	entries  []yamlMappingEntry
}

func missingYAMLMappingEntries(existing, generated *yaml.Node) ([]yamlMappingEntry, error) {
	if existing != nil && existing.Kind != yaml.MappingNode {
		return nil, errors.New("stbench.adapters must be a YAML mapping")
	}

	missing := make([]yamlMappingEntry, 0, len(generated.Content)/2)
	for index := 0; index+1 < len(generated.Content); index += 2 {
		key := generated.Content[index].Value
		if existing != nil && hasYAMLMappingKey(existing, key) {
			continue
		}
		value := generated.Content[index+1]
		if value.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("generated adapter %q command must be a scalar", key)
		}
		missing = append(missing, yamlMappingEntry{key: key, value: value.Value})
	}

	return missing, nil
}

func (a yamlMappingAppender) appendEntries() ([]byte, error) {
	if a.mapping.Style&yaml.FlowStyle != 0 {
		return a.appendFlowEntries()
	}
	if len(a.mapping.Content) == 0 {
		return nil, errors.New("cannot locate empty block-style adapters mapping")
	}

	lastValue := a.mapping.Content[len(a.mapping.Content)-1]
	insertion, err := yamlLineEnd(a.contents, lastValue.Line)
	if err != nil {
		return nil, err
	}
	indent := a.mapping.Content[0].Column - 1
	addition := formatBlockYAMLMappingEntries(a.entries, indent)

	return a.insertAtOffset(insertion, addition), nil
}

func (a yamlMappingAppender) appendFlowEntries() ([]byte, error) {
	start, err := yamlLineStart(a.contents, a.mapping.Line)
	if err != nil {
		return nil, err
	}
	lineEnd, err := yamlLineEnd(a.contents, a.mapping.Line)
	if err != nil {
		return nil, err
	}
	searchStart := start + a.mapping.Column - 1
	closingRelative := bytes.LastIndexByte(a.contents[searchStart:lineEnd], '}')
	if closingRelative < 0 {
		return nil, errors.New("cannot locate closing brace for flow-style adapters mapping")
	}
	insertion := searchStart + closingRelative
	addition := formatFlowYAMLMappingEntries(a.entries)
	if len(a.mapping.Content) > 0 {
		addition = append([]byte(", "), addition...)
	}

	return a.insertAtOffset(insertion, addition), nil
}

func (a yamlMappingAppender) appendMapping(path string, root *yaml.Node) ([]byte, error) {
	if a.mapping.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("decode %s: cannot extend flow-style stbench without adapters", path)
	}

	stbenchIndex := yamlMappingKeyIndex(root, "stbench")
	if stbenchIndex < 0 {
		return nil, fmt.Errorf("decode %s: missing stbench mapping", path)
	}
	insertion := len(a.contents)
	if stbenchIndex+2 < len(root.Content) {
		var err error
		insertion, err = yamlLineStart(a.contents, root.Content[stbenchIndex+2].Line)
		if err != nil {
			return nil, err
		}
	}
	indent := root.Content[stbenchIndex].Column - 1 + 2
	if len(a.mapping.Content) > 0 {
		indent = a.mapping.Content[0].Column - 1
	}
	addition := []byte(strings.Repeat(" ", indent) + "adapters:\n")
	addition = append(addition, formatBlockYAMLMappingEntries(a.entries, indent+2)...)

	return a.insertAtOffset(insertion, addition), nil
}

func (a yamlMappingAppender) insertAtOffset(insertion int, addition []byte) []byte {
	if insertion == len(a.contents) && len(a.contents) > 0 && a.contents[len(a.contents)-1] != '\n' {
		addition = append([]byte("\n"), addition...)
	}

	return insertBytes(a.contents, insertion, addition)
}

func formatBlockYAMLMappingEntries(entries []yamlMappingEntry, indent int) []byte {
	var formatted strings.Builder
	for _, entry := range entries {
		formatted.WriteString(strings.Repeat(" ", indent))
		formatted.WriteString(entry.key)
		formatted.WriteString(": ")
		formatted.WriteString(strconv.Quote(entry.value))
		formatted.WriteByte('\n')
	}
	return []byte(formatted.String())
}

func formatFlowYAMLMappingEntries(entries []yamlMappingEntry) []byte {
	formatted := make([]string, 0, len(entries))
	for _, entry := range entries {
		formatted = append(formatted, entry.key+": "+strconv.Quote(entry.value))
	}
	return []byte(strings.Join(formatted, ", "))
}

func yamlLineStart(contents []byte, line int) (int, error) {
	if line < 1 {
		return 0, fmt.Errorf("invalid YAML line %d", line)
	}
	offset := 0
	for current := 1; current < line; current++ {
		relative := bytes.IndexByte(contents[offset:], '\n')
		if relative < 0 {
			return 0, fmt.Errorf("YAML line %d is outside document", line)
		}
		offset += relative + 1
	}
	return offset, nil
}

func yamlLineEnd(contents []byte, line int) (int, error) {
	start, err := yamlLineStart(contents, line)
	if err != nil {
		return 0, err
	}
	relative := bytes.IndexByte(contents[start:], '\n')
	if relative < 0 {
		return len(contents), nil
	}
	return start + relative + 1, nil
}

func insertBytes(contents []byte, offset int, addition []byte) []byte {
	updated := make([]byte, 0, len(contents)+len(addition))
	updated = append(updated, contents[:offset]...)
	updated = append(updated, addition...)
	updated = append(updated, contents[offset:]...)
	return updated
}

func writeYAMLDocument(path string, document *yaml.Node) error {
	updated, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func yamlMappingRoot(document *yaml.Node) (*yaml.Node, error) {
	if len(document.Content) == 0 {
		return nil, errors.New("top level must be a YAML mapping")
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("top level must be a YAML mapping")
	}

	return root, nil
}

func hasYAMLMappingKey(mapping *yaml.Node, key string) bool {
	return yamlMappingKeyIndex(mapping, key) >= 0
}

func yamlMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	index := yamlMappingKeyIndex(mapping, key)
	if index < 0 {
		return nil
	}

	return mapping.Content[index+1]
}

func yamlMappingKeyIndex(mapping *yaml.Node, key string) int {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return index
		}
	}

	return -1
}

func ensureManagedStateIgnored(repositoryDir string) error {
	path := filepath.Join(repositoryDir, ".gitignore")
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		contents = nil
	} else if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if gitignoreContainsManagedState(string(contents)) {
		return nil
	}

	addition := "# stbench managed state\n" + managedStateGitignoreEntry + "\n"
	if len(contents) > 0 && !strings.HasSuffix(string(contents), "\n") {
		addition = "\n" + addition
	}
	updated := append(append([]byte(nil), contents...), []byte(addition)...)
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func gitignoreContainsManagedState(contents string) bool {
	for _, line := range strings.Split(contents, "\n") {
		switch strings.TrimSpace(line) {
		case managedStateGitignoreEntry, "/" + managedStateGitignoreEntry, ".local/", "/.local/":
			return true
		}
	}
	return false
}

func managedHarnessDir(repositoryDir string) (string, error) {
	absoluteRepositoryDir, err := filepath.Abs(repositoryDir)
	if err != nil {
		return "", fmt.Errorf("resolve API repository directory: %w", err)
	}

	stateRoot := strings.TrimSpace(os.Getenv(stbenchStateDirEnv))
	if stateRoot == "" {
		return filepath.Join(absoluteRepositoryDir, ".local", "stbench"), nil
	}
	if !filepath.IsAbs(stateRoot) {
		stateRoot = filepath.Join(absoluteRepositoryDir, stateRoot)
	}
	harnessDir, err := filepath.Abs(stateRoot)
	if err != nil {
		return "", fmt.Errorf("resolve stbench state directory: %w", err)
	}
	defaultHarnessDir := filepath.Join(absoluteRepositoryDir, ".local", "stbench")
	if pathWithin(absoluteRepositoryDir, harnessDir) && harnessDir != defaultHarnessDir {
		return "", fmt.Errorf("repository-local state must use .local/stbench; got %q", harnessDir)
	}

	return harnessDir, nil
}

func pathWithin(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func stbenchConfigBlockFor(harnessDir string) string {
	return stbenchConfigBlockForAdapters(harnessDir, stbenchadapters.Files())
}

func stbenchConfigBlockForAdapters(harnessDir string, adapterFiles []stbenchadapters.AdapterFile) string {
	path := func(name string) string {
		return strconv.Quote(quoteShellPath(filepath.Join(harnessDir, name)))
	}
	adapterCommand := func(name string) string {
		adapterPath := filepath.Join(harnessDir, "adapters", name)
		return strconv.Quote("python " + quoteShellPath(adapterPath))
	}
	adapterConfigLines := make([]string, 0, len(adapterFiles))
	for _, adapter := range adapterFiles {
		if adapter.Role == "" {
			continue
		}
		line := fmt.Sprintf("    %s: %s", adapter.Role, adapterCommand(adapter.Filename))
		adapterConfigLines = append(adapterConfigLines, line)
	}
	adapterConfig := "  adapters:\n" + strings.Join(adapterConfigLines, "\n")
	if len(adapterConfigLines) == 0 {
		adapterConfig = "  adapters: {}"
	}

	return fmt.Sprintf(`# The lifecycle scripts live in the managed stbench state directory:
# %s
stbench:
  hardware: hardware-name
%s
  adapter_timeout: 30m
  reuse_process: false
  source_dir: .
  stcompare_binary: stcompare
  prompt:
    id: stbench-default
    version: "2"
  lifecycle:
    stop: %s
    reset: %s
    build: %s
    start: %s
    command_timeout: 30m
    health_url: http://localhost:8080/health
    health_timeout: 30s
    health_interval: 100ms
  max_iterations: 100
  stall_window: 2
`, harnessDir,
		adapterConfig,
		path("stop.sh"),
		path("reset.sh"),
		path("build.sh"),
		path("start.sh"),
	)
}

func quoteShellPath(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeLifecycleScaffold(directory string) ([]string, error) {
	created := make([]string, 0, len(lifecycleScaffold))
	for _, file := range lifecycleScaffold {
		path := filepath.Join(directory, file.name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			removeCreatedFiles(created)
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		if err := writeScaffoldFile(path, file.contents); err != nil {
			removeCreatedFiles(created)
			return nil, err
		}
		created = append(created, path)
	}

	return created, nil
}

func writeAdapterScaffold(harnessDir string, adapterFiles []stbenchadapters.AdapterFile) ([]string, error) {
	if len(adapterFiles) == 0 {
		return nil, nil
	}

	directory := filepath.Join(harnessDir, "adapters")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create adapter directory: %w", err)
	}

	created := make([]string, 0, len(adapterFiles))
	for _, adapter := range adapterFiles {
		if adapter.Role == "" {
			continue
		}
		contents, err := adapter.Contents()
		if err != nil {
			removeCreatedFiles(created)
			return nil, fmt.Errorf("read embedded adapter %s: %w", adapter.Filename, err)
		}
		path := filepath.Join(directory, adapter.Filename)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			removeCreatedFiles(created)
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		if err := writeManagedFile(path, contents, 0o644); err != nil {
			removeCreatedFiles(created)
			return nil, err
		}
		created = append(created, path)
	}

	return created, nil
}

func shouldResyncProtocol(
	harnessDir string,
	adapterFiles []stbenchadapters.AdapterFile,
) (bool, error) {
	for _, adapter := range adapterFiles {
		if adapter.Role != "" {
			return true, nil
		}
	}

	adapterDir := filepath.Join(harnessDir, "adapters")
	entries, err := os.ReadDir(adapterDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read adapter directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "_protocol.py" {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".py") {
			return true, nil
		}
	}

	return false, nil
}

func resyncProtocol(harnessDir string) error {
	var protocol stbenchadapters.AdapterFile
	for _, adapter := range stbenchadapters.Files() {
		if adapter.Role == "" {
			protocol = adapter
			break
		}
	}
	contents, err := protocol.Contents()
	if err != nil {
		return fmt.Errorf("read embedded adapter %s: %w", protocol.Filename, err)
	}

	directory := filepath.Join(harnessDir, "adapters")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create adapter directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".protocol-*")
	if err != nil {
		return fmt.Errorf("create temporary protocol: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o644); err != nil {
		return fmt.Errorf("chmod temporary protocol: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary protocol: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary protocol: %w", err)
	}
	path := filepath.Join(directory, protocol.Filename)
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	committed = true

	return nil
}

func writeScaffoldFile(path string, contents string) error {
	return writeManagedFile(path, []byte(contents), 0o755)
}

func writeManagedFile(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", filepath.Base(path))
	}
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()

	if _, err := file.Write(contents); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	committed = true

	return nil
}

func removeCreatedFiles(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func newFileSnapshot(path string) (fileSnapshot, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("read %s: %w", path, err)
	}

	return fileSnapshot{path: path, contents: contents, existed: true}, nil
}

func rollbackInit(created []string, snapshot fileSnapshot, cause error) error {
	removeCreatedFiles(created)
	if err := snapshot.restoreIfChanged(); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (s fileSnapshot) restoreIfChanged() error {
	current, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if !s.existed {
			return nil
		}
		if err := os.WriteFile(s.path, s.contents, 0o644); err != nil {
			return fmt.Errorf("restore %s: %w", s.path, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s for rollback: %w", s.path, err)
	}
	if bytes.Equal(current, s.contents) {
		return nil
	}
	if !s.existed {
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("remove %s during rollback: %w", s.path, err)
		}
		return nil
	}
	if err := os.WriteFile(s.path, s.contents, 0o644); err != nil {
		return fmt.Errorf("restore %s: %w", s.path, err)
	}
	return nil
}
