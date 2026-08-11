package bench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
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

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "init",
		Args: cobra.NoArgs,
		RunE: runInit,
	}
}

func runInit(command *cobra.Command, _ []string) error {
	repositoryDir := "."
	var err error
	harnessDir, err := managedHarnessDir(repositoryDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		return fmt.Errorf("create managed stbench directory: %w", err)
	}

	created, err := writeLifecycleScaffold(harnessDir)
	if err != nil {
		return err
	}
	adapterFiles, err := writeAdapterScaffold(harnessDir)
	if err != nil {
		removeCreatedFiles(created)
		return err
	}
	created = append(created, adapterFiles...)
	if err := ensureManagedStateIgnored(repositoryDir); err != nil {
		removeCreatedFiles(created)
		return fmt.Errorf("update .gitignore: %w", err)
	}

	configPath := filepath.Join(repositoryDir, config.DefaultFilename)
	switch err := appendStbenchConfig(configPath, stbenchConfigBlockFor(harnessDir)); {
	case errors.Is(err, errStbenchConfigPresent):
		// stcompare config init is the authoritative writer of the stbench block.
		// When one is already present, keep the freshly created lifecycle scripts
		// and leave the block untouched instead of failing and rolling them back.
		if _, err := fmt.Fprintf(command.OutOrStdout(), "%s already has an stbench block; kept the lifecycle scripts and left the block unchanged\n", configPath); err != nil {
			return fmt.Errorf("write init confirmation: %w", err)
		}
	case err != nil:
		removeCreatedFiles(created)
		return err
	default:
		if _, err := fmt.Fprintf(command.OutOrStdout(), "Wrote stbench configuration to %s\n", configPath); err != nil {
			return fmt.Errorf("write init confirmation: %w", err)
		}
	}

	return nil
}

// errStbenchConfigPresent signals that stcompare.yaml already carries an stbench
// block, so init leaves it unchanged rather than treating it as a failure.
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
	if hasYAMLMappingKey(root, "stbench") {
		return errStbenchConfigPresent
	}

	var blockDocument yaml.Node
	if err := yaml.Unmarshal([]byte(block), &blockDocument); err != nil {
		return fmt.Errorf("decode generated stbench config: %w", err)
	}
	blockRoot, err := yamlMappingRoot(&blockDocument)
	if err != nil {
		return fmt.Errorf("decode generated stbench config: %w", err)
	}
	root.Content = append(root.Content, blockRoot.Content...)

	updated, err := yaml.Marshal(&document)
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
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return true
		}
	}

	return false
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
	path := func(name string) string {
		return strconv.Quote(quoteShellPath(filepath.Join(harnessDir, name)))
	}
	adapterCommand := func(name string) string {
		adapterPath := filepath.Join(harnessDir, "adapters", name)
		return strconv.Quote("python " + quoteShellPath(adapterPath))
	}
	adapterConfigLines := make([]string, 0, 3)
	for _, adapter := range stbenchadapters.Files() {
		if adapter.Role == "" {
			continue
		}
		line := fmt.Sprintf("    %s: %s", adapter.Role, adapterCommand(adapter.Filename))
		adapterConfigLines = append(adapterConfigLines, line)
	}

	return fmt.Sprintf(`# The lifecycle scripts live in the managed stbench state directory:
# %s
stbench:
  hardware: hardware-name
  adapters:
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
		strings.Join(adapterConfigLines, "\n"),
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
		if err := writeScaffoldFile(path, file.contents); err != nil {
			removeCreatedFiles(created)
			return nil, err
		}
		created = append(created, path)
	}

	return created, nil
}

func writeAdapterScaffold(harnessDir string) ([]string, error) {
	directory := filepath.Join(harnessDir, "adapters")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create adapter directory: %w", err)
	}

	adapterFiles := stbenchadapters.Files()
	created := make([]string, 0, len(adapterFiles))
	for _, adapter := range adapterFiles {
		contents, err := adapter.Contents()
		if err != nil {
			removeCreatedFiles(created)
			return nil, fmt.Errorf("read embedded adapter %s: %w", adapter.Filename, err)
		}
		path := filepath.Join(directory, adapter.Filename)
		if err := writeManagedFile(path, contents, 0o644); err != nil {
			removeCreatedFiles(created)
			return nil, err
		}
		created = append(created, path)
	}

	return created, nil
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
