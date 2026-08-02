package bench

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

const lifecycleConfigStanza = `# Add this stanza to stcompare.yaml and adjust the candidate, adapter, and health URL.
stbench:
  candidate: gpt5.6
  agent: local-agent
  adapter: ./adapter.sh
  candidate_dir: .
  stcompare_binary: stcompare
  record_path: benchmark-record.json
  lifecycle:
    stop: ./stop.sh
    reset: ./reset.sh
    build: ./build.sh
    start: ./start.sh
    health_url: http://localhost:8080/health
    health_timeout: 30s
    health_interval: 100ms
  max_iterations: 100
  stall_window: 2
`

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "init",
		Args: cobra.NoArgs,
		RunE: runInit,
	}
}

func runInit(command *cobra.Command, _ []string) error {
	created, err := writeLifecycleScaffold(".")
	if err != nil {
		return err
	}

	if _, err := io.WriteString(command.OutOrStdout(), lifecycleConfigStanza); err != nil {
		removeCreatedFiles(created)
		return fmt.Errorf("write stbench config stanza: %w", err)
	}

	return nil
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
