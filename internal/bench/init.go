package bench

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
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
	if err := ensureManagedStateIgnored(repositoryDir); err != nil {
		removeCreatedFiles(created)
		return fmt.Errorf("update .gitignore: %w", err)
	}

	stanza := lifecycleConfigStanzaFor(harnessDir)
	if _, err := io.WriteString(command.OutOrStdout(), stanza); err != nil {
		removeCreatedFiles(created)
		return fmt.Errorf("write stbench config stanza: %w", err)
	}

	return nil
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

func lifecycleConfigStanzaFor(harnessDir string) string {
	path := func(name string) string {
		return strconv.Quote(quoteShellPath(filepath.Join(harnessDir, name)))
	}

	return fmt.Sprintf(`# Add this stanza to stcompare.yaml and adjust the candidate, adapter, and health URL.
# The lifecycle scripts and benchmark record live in the managed stbench state directory:
# %s
stbench:
  candidate: gpt5.6
  agent: local-agent
  model: model-name
  hardware: hardware-name
  # Keep the adapter command outside the API repository as well.
  adapter: python /absolute/path/to/adapter.py
  adapter_timeout: 30m
  candidate_dir: .
  stcompare_binary: stcompare
  record_path: %s
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
		strconv.Quote(filepath.Join(harnessDir, "benchmark-record.json")),
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

func writeScaffoldFile(path string, contents string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
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

	if _, err := io.WriteString(file, contents); err != nil {
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
