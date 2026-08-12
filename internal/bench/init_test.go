package bench

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	stbenchadapters "stcompare/examples/stbench"
	"stcompare/internal/config"
)

func TestInitCreatesLifecycleScaffoldAndWritesLoadableConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	var output bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	for _, name := range []string{"stop.sh", "reset.sh", "build.sh", "start.sh"} {
		path := filepath.Join(harnessDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %o, want 755", name, info.Mode().Perm())
		}
		if err := exec.Command("sh", "-n", path).Run(); err != nil {
			t.Errorf("shell-check %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists in API source tree, want managed harness only", name)
		}
	}
	gitignore := readInitFile(t, filepath.Join(dir, ".gitignore"))
	if !strings.Contains(gitignore, ".local/stbench/") {
		t.Fatalf(".gitignore = %q, want .local/stbench/ entry", gitignore)
	}

	stopScript := readInitFile(t, filepath.Join(harnessDir, "stop.sh"))
	if !strings.Contains(stopScript, "stop must no-op cleanly when nothing is running") {
		t.Errorf("stop.sh = %q, want no-process safety comment", stopScript)
	}
	if err := exec.Command("sh", filepath.Join(harnessDir, "stop.sh")).Run(); err != nil {
		t.Errorf("first stop.sh run: %v", err)
	}
	if err := exec.Command("sh", filepath.Join(harnessDir, "stop.sh")).Run(); err != nil {
		t.Errorf("second stop.sh run: %v", err)
	}

	resetScript := readInitFile(t, filepath.Join(harnessDir, "reset.sh"))
	if !strings.Contains(resetScript, "reset must not revert source changes") {
		t.Errorf("reset.sh = %q, want source-preservation warning", resetScript)
	}

	configContents := readInitFile(t, filepath.Join(dir, config.DefaultFilename))
	for _, want := range []string{
		"stbench:",
		"adapters:",
		"cloud:",
		"local:",
		"coding-agent:",
		"hardware: hardware-name",
		"prompt:",
		"source_dir: .",
		filepath.Join(harnessDir, "stop.sh"),
		filepath.Join(harnessDir, "reset.sh"),
		filepath.Join(harnessDir, "build.sh"),
		filepath.Join(harnessDir, "start.sh"),
	} {
		if !strings.Contains(configContents, want) {
			t.Errorf("config = %q, want %q", configContents, want)
		}
	}
	var document struct {
		Stbench map[string]any `yaml:"stbench"`
	}
	if err := yaml.Unmarshal([]byte(configContents), &document); err != nil {
		t.Fatalf("parse written config: %v", err)
	}
	for _, unwanted := range []string{"record_path", "campaign", "agent", "model", "adapter"} {
		if _, exists := document.Stbench[unwanted]; exists {
			t.Errorf("stbench config contains obsolete field %q", unwanted)
		}
	}

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("validate written config: %v", err)
	}
	if got, want := output.String(), "Wrote stbench configuration to stcompare.yaml\n"; got != want {
		t.Fatalf("init output = %q, want %q", got, want)
	}
}

func TestInitInstallsCanonicalAdapterFiles(t *testing.T) {
	adapterNames := []string{
		"_protocol.py",
		"adapter.py",
		"coding_agent_adapter.py",
		"local_model_adapter.py",
	}
	canonicalContents := make(map[string][]byte, len(adapterNames))
	for _, name := range adapterNames {
		contents, err := os.ReadFile(filepath.Join("..", "..", "examples", "stbench", name))
		if err != nil {
			t.Fatalf("read canonical adapter %s: %v", name, err)
		}
		canonicalContents[name] = contents
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	adapterDir := filepath.Join(harnessDir, "adapters")
	entries, err := os.ReadDir(adapterDir)
	if err != nil {
		t.Fatalf("read installed adapter directory: %v", err)
	}
	if got, want := len(entries), len(adapterNames); got != want {
		t.Fatalf("installed adapter count = %d, want %d", got, want)
	}

	for _, entry := range entries {
		wantContents, exists := canonicalContents[entry.Name()]
		if !exists {
			t.Errorf("unexpected installed adapter %q", entry.Name())
			continue
		}
		path := filepath.Join(adapterDir, entry.Name())
		gotContents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("read installed adapter %s: %v", entry.Name(), readErr)
			continue
		}
		if !bytes.Equal(gotContents, wantContents) {
			t.Errorf("installed adapter %s does not match canonical source", entry.Name())
		}
		info, statErr := entry.Info()
		if statErr != nil {
			t.Errorf("stat installed adapter %s: %v", entry.Name(), statErr)
			continue
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
			t.Errorf("installed adapter %s mode = %o, want %o", entry.Name(), got, want)
		}
	}
}

func TestInitConfiguresInstalledAdapters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "API repo's files")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("create repository directory: %v", err)
	}
	t.Chdir(dir)
	configPath := filepath.Join(dir, config.DefaultFilename)
	writeInitConfigWithoutStbench(t, configPath)

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	want := map[string]string{
		"cloud":        "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "adapter.py")),
		"coding-agent": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "coding_agent_adapter.py")),
		"local":        "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, want) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, want)
	}
}

func TestInitSelectsSingleAdapter(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")

	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"local_model_adapter.py",
	})

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	want := map[string]string{
		"local": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, want) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, want)
	}
}

func TestInitExplicitAdaptersTakePrecedenceInInteractiveTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	selectorErr := errors.New("adapter selector must not be called")
	command := newInitCommand(initDependencies{
		isTerminal: func(int) bool { return true },
		selectAdapters: func(*cobra.Command) ([]stbenchadapters.Role, error) {
			return nil, selectorErr
		},
	})
	command.SetArgs([]string{"--adapters=local"})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute stbench init with explicit adapters: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"local_model_adapter.py",
	})
}

func TestInitNonInteractiveTerminalInstallsAllAdaptersWithoutPrompting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	selectorErr := errors.New("adapter selector must not be called")
	command := newInitCommand(initDependencies{
		isTerminal: func(int) bool { return false },
		selectAdapters: func(*cobra.Command) ([]stbenchadapters.Role, error) {
			return nil, selectorErr
		},
	})
	command.SetArgs([]string{})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute non-interactive stbench init: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"adapter.py",
		"coding_agent_adapter.py",
		"local_model_adapter.py",
	})
}

func TestInitInteractiveSelectionInstallsAndConfiguresSelectedAdapters(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	command := newInitCommand(initDependencies{
		isTerminal: func(int) bool { return true },
		selectAdapters: func(*cobra.Command) ([]stbenchadapters.Role, error) {
			return []stbenchadapters.Role{stbenchadapters.RoleLocal}, nil
		},
	})
	command.SetArgs([]string{})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interactive stbench init: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"local_model_adapter.py",
	})

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	want := map[string]string{
		"local": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, want) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, want)
	}
}

func TestInitInteractiveEmptySelectionCreatesNoAdapterDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	command := newInitCommand(initDependencies{
		isTerminal: func(int) bool { return true },
		selectAdapters: func(*cobra.Command) ([]stbenchadapters.Role, error) {
			return []stbenchadapters.Role{}, nil
		},
	})
	command.SetArgs([]string{})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interactive stbench init with empty selection: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	for _, name := range []string{"stop.sh", "reset.sh", "build.sh", "start.sh"} {
		if _, err := os.Stat(filepath.Join(harnessDir, name)); err != nil {
			t.Errorf("stat lifecycle script %s: %v", name, err)
		}
	}

	var document struct {
		Stbench struct {
			Adapters map[string]string `yaml:"adapters"`
		} `yaml:"stbench"`
	}
	contents := readInitFile(t, filepath.Join(dir, config.DefaultFilename))
	if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
		t.Fatalf("parse written config: %v", err)
	}
	if len(document.Stbench.Adapters) != 0 {
		t.Fatalf("configured adapters = %#v, want empty map", document.Stbench.Adapters)
	}

	adapterDir := filepath.Join(harnessDir, "adapters")
	if _, err := os.Stat(adapterDir); !os.IsNotExist(err) {
		t.Fatalf("adapter directory exists after empty interactive selection: %v", err)
	}
}

func TestInitPromptsOnlyWhenStdinAndStdoutAreTerminals(t *testing.T) {
	const (
		stdinFD  = 10
		stdoutFD = 11
	)
	tests := []struct {
		name      string
		stdinTTY  bool
		stdoutTTY bool
		wantFiles []string
	}{
		{
			name:      "both terminals",
			stdinTTY:  true,
			stdoutTTY: true,
			wantFiles: []string{"_protocol.py", "local_model_adapter.py"},
		},
		{
			name:      "only stdin terminal",
			stdinTTY:  true,
			stdoutTTY: false,
			wantFiles: []string{"_protocol.py", "adapter.py", "coding_agent_adapter.py", "local_model_adapter.py"},
		},
		{
			name:      "only stdout terminal",
			stdinTTY:  false,
			stdoutTTY: true,
			wantFiles: []string{"_protocol.py", "adapter.py", "coding_agent_adapter.py", "local_model_adapter.py"},
		},
		{
			name:      "neither terminal",
			stdinTTY:  false,
			stdoutTTY: false,
			wantFiles: []string{"_protocol.py", "adapter.py", "coding_agent_adapter.py", "local_model_adapter.py"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

			command := newInitCommand(initDependencies{
				isTerminal: func(fd int) bool {
					switch fd {
					case stdinFD:
						return test.stdinTTY
					case stdoutFD:
						return test.stdoutTTY
					default:
						return false
					}
				},
				selectAdapters: func(*cobra.Command) ([]stbenchadapters.Role, error) {
					return []stbenchadapters.Role{stbenchadapters.RoleLocal}, nil
				},
			})
			command.SetIn(fakeTerminalStream{fd: stdinFD})
			command.SetOut(fakeTerminalStream{fd: stdoutFD})
			command.SetArgs([]string{})
			if err := command.Execute(); err != nil {
				t.Fatalf("execute stbench init: %v", err)
			}

			harnessDir, err := managedHarnessDir(dir)
			if err != nil {
				t.Fatalf("resolve managed harness directory: %v", err)
			}
			assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), test.wantFiles)
		})
	}
}

func TestInitDefaultSelectorOffersAllAdaptersUnselected(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	stdin := &fakeTerminalStream{fd: 20}
	stdout := &fakeTerminalStream{fd: 21}
	runner := checklistRunner(func(
		input io.Reader,
		output io.Writer,
		choices []adapterChoice,
	) ([]stbenchadapters.Role, error) {
		if input != stdin {
			t.Errorf("checklist input differs from command input")
		}
		if output != stdout {
			t.Errorf("checklist output differs from command output")
		}
		wantChoices := []adapterChoice{
			{label: "cloud", role: stbenchadapters.RoleCloud},
			{label: "local", role: stbenchadapters.RoleLocal},
			{label: "coding-agent", role: stbenchadapters.RoleCodingAgent},
		}
		if !slices.Equal(choices, wantChoices) {
			t.Errorf("adapter choices = %#v, want %#v", choices, wantChoices)
		}
		return []stbenchadapters.Role{stbenchadapters.RoleLocal}, nil
	})

	command := newInitCommand(initDependencies{
		isTerminal:     func(int) bool { return true },
		selectAdapters: newAdapterSelector(runner),
	})
	command.SetIn(stdin)
	command.SetOut(stdout)
	command.SetArgs([]string{})
	if err := command.Execute(); err != nil {
		t.Fatalf("execute interactive stbench init: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"local_model_adapter.py",
	})

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	wantAdapters := map[string]string{
		"local": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, wantAdapters) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, wantAdapters)
	}
}

func TestRootInitDependenciesDriveInteractiveSelection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	stdin := &fakeTerminalStream{fd: 30}
	stdout := &fakeTerminalStream{fd: 31}
	deps := newInitDependencies(
		func(fd int) bool { return fd == int(stdin.Fd()) || fd == int(stdout.Fd()) },
		func(
			_ io.Reader,
			_ io.Writer,
			_ []adapterChoice,
		) ([]stbenchadapters.Role, error) {
			return []stbenchadapters.Role{stbenchadapters.RoleLocal}, nil
		},
	)
	root := newRootCommand(deps)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute interactive stbench init through root: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"local_model_adapter.py",
	})

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	wantAdapters := map[string]string{
		"local": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, wantAdapters) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, wantAdapters)
	}
}

func TestInitSelectsMultipleAdapters(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=cloud,local")

	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"adapter.py",
		"local_model_adapter.py",
	})

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	want := map[string]string{
		"cloud": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "adapter.py")),
		"local": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, want) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, want)
	}
}

func TestInitRerunAddsOnlyMissingSelectedAdapters(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")
	adapterDir := filepath.Join(harnessDir, "adapters")
	localPath := filepath.Join(adapterDir, "local_model_adapter.py")
	protocolPath := filepath.Join(adapterDir, "_protocol.py")
	configPath := filepath.Join(dir, config.DefaultFilename)

	const localSentinel = "# user-edited local adapter\n"
	if err := os.WriteFile(localPath, []byte(localSentinel), 0o644); err != nil {
		t.Fatalf("write local adapter sentinel: %v", err)
	}
	localInfoBefore, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("stat local adapter before rerun: %v", err)
	}
	if err := os.WriteFile(protocolPath, []byte("# stale protocol\n"), 0o644); err != nil {
		t.Fatalf("write stale protocol: %v", err)
	}

	configBefore := readInitFile(t, configPath)
	const configuredLocal = "python 'keep-existing-local-adapter'"
	configBefore = strings.Replace(
		configBefore,
		"python '"+localPath+"'",
		configuredLocal,
		1,
	)
	if err := os.WriteFile(configPath, []byte(configBefore), 0o644); err != nil {
		t.Fatalf("write existing adapter config sentinel: %v", err)
	}

	var output bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"init", "--adapters=local,coding-agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rerun stbench init: %v", err)
	}

	if got := readInitFile(t, localPath); got != localSentinel {
		t.Fatalf("existing local adapter = %q, want untouched %q", got, localSentinel)
	}
	localInfoAfter, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("stat local adapter after rerun: %v", err)
	}
	if !localInfoAfter.ModTime().Equal(localInfoBefore.ModTime()) {
		t.Fatalf(
			"existing local adapter mtime = %v, want untouched %v",
			localInfoAfter.ModTime(),
			localInfoBefore.ModTime(),
		)
	}

	var canonicalProtocol []byte
	for _, adapter := range stbenchadapters.Files() {
		if adapter.Filename != "_protocol.py" {
			continue
		}
		canonicalProtocol, err = adapter.Contents()
		if err != nil {
			t.Fatalf("read embedded protocol: %v", err)
		}
		break
	}
	if got := []byte(readInitFile(t, protocolPath)); !bytes.Equal(got, canonicalProtocol) {
		t.Fatal("protocol was not resynchronized from the embedded copy")
	}
	if _, err := os.Stat(filepath.Join(adapterDir, "coding_agent_adapter.py")); err != nil {
		t.Fatalf("stat newly selected coding-agent adapter: %v", err)
	}

	configAfter := readInitFile(t, configPath)
	if !strings.Contains(configAfter, configuredLocal) {
		t.Fatalf("config = %q, want existing local adapter entry untouched", configAfter)
	}
	if !strings.Contains(configAfter, "coding-agent:") {
		t.Fatalf("config = %q, want new coding-agent entry", configAfter)
	}
	if !strings.Contains(output.String(), "Wrote stbench configuration") {
		t.Fatalf("init output = %q, want configuration-written confirmation", output.String())
	}
}

func TestInitRerunWithFullyInstalledAdapterOnlyResyncsProtocol(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")
	configPath := filepath.Join(dir, config.DefaultFilename)
	localPath := filepath.Join(harnessDir, "adapters", "local_model_adapter.py")
	protocolPath := filepath.Join(harnessDir, "adapters", "_protocol.py")
	configBefore := readInitFile(t, configPath)
	localInfoBefore, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("stat local adapter before rerun: %v", err)
	}
	oldProtocolTime := time.Unix(1, 0)
	if err := os.Chtimes(protocolPath, oldProtocolTime, oldProtocolTime); err != nil {
		t.Fatalf("set protocol mtime: %v", err)
	}

	var output bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"init", "--adapters=local"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rerun stbench init: %v", err)
	}

	if got := readInitFile(t, configPath); got != configBefore {
		t.Fatalf("config changed on no-op rerun\ngot:  %q\nwant: %q", got, configBefore)
	}
	localInfoAfter, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("stat local adapter after rerun: %v", err)
	}
	if !localInfoAfter.ModTime().Equal(localInfoBefore.ModTime()) {
		t.Fatalf("local adapter mtime = %v, want %v", localInfoAfter.ModTime(), localInfoBefore.ModTime())
	}
	protocolInfo, err := os.Stat(protocolPath)
	if err != nil {
		t.Fatalf("stat protocol after rerun: %v", err)
	}
	if protocolInfo.ModTime().Equal(oldProtocolTime) {
		t.Fatalf("protocol mtime = %v, want rewritten file", protocolInfo.ModTime())
	}
	wantOutput := "stcompare.yaml already has an stbench block; kept the lifecycle scripts and left the block unchanged\n"
	if got := output.String(); got != wantOutput {
		t.Fatalf("init output = %q, want %q", got, wantOutput)
	}
}

func TestInitEmptySelectionResyncsProtocolWhenAdaptersAlreadyExist(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")
	configPath := filepath.Join(dir, config.DefaultFilename)
	protocolPath := filepath.Join(harnessDir, "adapters", "_protocol.py")
	configBefore := readInitFile(t, configPath)
	if err := os.WriteFile(protocolPath, []byte("# stale protocol\n"), 0o644); err != nil {
		t.Fatalf("write stale protocol: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init", "--adapters="})
	if err := root.Execute(); err != nil {
		t.Fatalf("rerun stbench init with empty selection: %v", err)
	}

	if got := readInitFile(t, configPath); got != configBefore {
		t.Fatalf("config changed for empty selection\ngot:  %q\nwant: %q", got, configBefore)
	}
	if got := readInitFile(t, protocolPath); got == "# stale protocol\n" {
		t.Fatal("protocol was not resynchronized for populated adapter directory")
	}
}

func TestInitAddsFirstAdapterToExistingEmptyMap(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=")
	configPath := filepath.Join(dir, config.DefaultFilename)
	configBefore := readInitFile(t, configPath)
	if !strings.Contains(configBefore, "  adapters: {}") {
		t.Fatalf("initial config = %q, want empty adapters map", configBefore)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init", "--adapters=local"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rerun stbench init with local adapter: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	wantCommand := "python " + quoteShellPath(
		filepath.Join(harnessDir, "adapters", "local_model_adapter.py"),
	)
	if got := loaded.Stbench.Adapters["local"]; got != wantCommand {
		t.Fatalf("configured local adapter = %q, want %q", got, wantCommand)
	}
	if _, err := os.Stat(filepath.Join(harnessDir, "adapters", "_protocol.py")); err != nil {
		t.Fatalf("stat synchronized protocol: %v", err)
	}
}

func TestInitAppendsAdapterConfigWithoutReformattingExistingBlock(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")
	configPath := filepath.Join(dir, config.DefaultFilename)
	const existing = `# keep leading comment
schema: openapi.json

stbench:
  hardware: "keep-quoted" # keep inline comment
  adapters:
    local: "keep-local-command" # keep adapter comment
  custom-setting: keep-me
`
	if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write formatted config: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init", "--adapters=local,coding-agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rerun stbench init: %v", err)
	}

	codingPath := filepath.Join(harnessDir, "adapters", "coding_agent_adapter.py")
	addedLine := "    coding-agent: " + strconv.Quote("python "+quoteShellPath(codingPath)) + "\n"
	got := readInitFile(t, configPath)
	withoutAddition := strings.Replace(got, addedLine, "", 1)
	if withoutAddition != existing {
		t.Fatalf("existing config formatting changed\ngot:  %q\nwant: %q", withoutAddition, existing)
	}
}

func TestInitReportsWhenItRestoresAMissingAdapterFile(t *testing.T) {
	_, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")
	localPath := filepath.Join(harnessDir, "adapters", "local_model_adapter.py")
	if err := os.Remove(localPath); err != nil {
		t.Fatalf("remove installed local adapter: %v", err)
	}

	var output bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"init", "--adapters=local"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rerun stbench init: %v", err)
	}

	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("stat restored local adapter: %v", err)
	}
	if got := output.String(); !strings.Contains(got, "Installed missing adapter files") {
		t.Fatalf("init output = %q, want installed-files confirmation", got)
	}
}

func TestInitFailedRerunRollsBackOnlyNewAdapterFiles(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local")
	adapterDir := filepath.Join(harnessDir, "adapters")
	localPath := filepath.Join(adapterDir, "local_model_adapter.py")
	protocolPath := filepath.Join(adapterDir, "_protocol.py")
	configPath := filepath.Join(dir, config.DefaultFilename)
	const localSentinel = "# keep prior adapter\n"
	const protocolSentinel = "# keep prior protocol on failed rerun\n"
	if err := os.WriteFile(localPath, []byte(localSentinel), 0o644); err != nil {
		t.Fatalf("write local adapter sentinel: %v", err)
	}
	if err := os.WriteFile(protocolPath, []byte(protocolSentinel), 0o644); err != nil {
		t.Fatalf("write protocol sentinel: %v", err)
	}
	configBefore := readInitFile(t, configPath)
	if err := os.Chmod(configPath, 0o444); err != nil {
		t.Fatalf("make config read-only: %v", err)
	}
	probe, probeErr := os.OpenFile(configPath, os.O_WRONLY, 0)
	if probeErr == nil {
		if err := probe.Close(); err != nil {
			t.Fatalf("close write-permission probe: %v", err)
		}
		t.Skip("file permissions do not prevent writes in this environment")
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init", "--adapters=local,coding-agent"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "write stcompare.yaml") {
		t.Fatalf("rerun stbench init error = %v, want config-write error", err)
	}

	if got := readInitFile(t, localPath); got != localSentinel {
		t.Fatalf("existing local adapter = %q, want %q", got, localSentinel)
	}
	if got := readInitFile(t, protocolPath); got != protocolSentinel {
		t.Fatalf("existing protocol = %q, want %q", got, protocolSentinel)
	}
	if _, err := os.Stat(filepath.Join(adapterDir, "coding_agent_adapter.py")); !os.IsNotExist(err) {
		t.Fatalf("new coding-agent adapter remains after failed rerun: %v", err)
	}
	if got := readInitFile(t, configPath); got != configBefore {
		t.Fatalf("config changed after failed rerun\ngot:  %q\nwant: %q", got, configBefore)
	}
}

func TestInitProtocolFailureRollsBackNewAdapterAndConfigEntry(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=")
	configPath := filepath.Join(dir, config.DefaultFilename)
	configBefore := readInitFile(t, configPath)
	adapterDir := filepath.Join(harnessDir, "adapters")
	protocolPath := filepath.Join(adapterDir, "_protocol.py")
	if err := os.MkdirAll(protocolPath, 0o755); err != nil {
		t.Fatalf("create blocking protocol directory: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init", "--adapters=local"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("rerun stbench init error = %v, want protocol-replace error", err)
	}

	localPath := filepath.Join(adapterDir, "local_model_adapter.py")
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("new local adapter remains after protocol failure: %v", err)
	}
	if got := readInitFile(t, configPath); got != configBefore {
		t.Fatalf("config changed after protocol failure\ngot:  %q\nwant: %q", got, configBefore)
	}
	info, err := os.Stat(protocolPath)
	if err != nil {
		t.Fatalf("stat prior protocol directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("prior protocol directory was modified during rollback")
	}
}

func TestInitDeduplicatesAdaptersAndIgnoresEmptyTokens(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=local,local,")

	assertInstalledAdapterNames(t, filepath.Join(harnessDir, "adapters"), []string{
		"_protocol.py",
		"local_model_adapter.py",
	})

	loaded, err := config.Load(filepath.Join(dir, config.DefaultFilename))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	want := map[string]string{
		"local": "python " + quoteShellPath(filepath.Join(harnessDir, "adapters", "local_model_adapter.py")),
	}
	if !maps.Equal(loaded.Stbench.Adapters, want) {
		t.Fatalf("configured adapters = %#v, want %#v", loaded.Stbench.Adapters, want)
	}
}

func TestInitWithOnlyEmptyAdapterTokensDoesNotCreateAdapterDirectory(t *testing.T) {
	dir, harnessDir := executeInitInTempRepository(t, "init", "--adapters=,")

	if _, err := os.Stat(filepath.Join(harnessDir, "adapters")); !os.IsNotExist(err) {
		t.Fatalf("adapter directory exists for empty selection: %v", err)
	}

	var document struct {
		Stbench struct {
			Adapters map[string]string `yaml:"adapters"`
		} `yaml:"stbench"`
	}
	contents := readInitFile(t, filepath.Join(dir, config.DefaultFilename))
	if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
		t.Fatalf("parse written config: %v", err)
	}
	if len(document.Stbench.Adapters) != 0 {
		t.Fatalf("configured adapters = %#v, want empty map", document.Stbench.Adapters)
	}
}

func TestInitRejectsUnknownAdapterBeforeWritingFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := filepath.Join(dir, config.DefaultFilename)
	writeInitConfigWithoutStbench(t, configPath)
	originalConfig := readInitFile(t, configPath)

	root := NewRootCommand()
	root.SetArgs([]string{"init", "--adapters=locall"})
	err := root.Execute()
	if err == nil {
		t.Fatal("execute stbench init error = nil, want unknown-adapter error")
	}
	for _, want := range []string{"locall", "cloud", "local", "coding-agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("execute stbench init error = %q, want %q", err, want)
		}
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	if _, statErr := os.Stat(harnessDir); !os.IsNotExist(statErr) {
		t.Fatalf("managed harness directory exists after invalid selection, want no files: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(statErr) {
		t.Fatalf(".gitignore exists after invalid selection, want no files: %v", statErr)
	}
	if got := readInitFile(t, configPath); got != originalConfig {
		t.Fatalf("config = %q, want unchanged content %q", got, originalConfig)
	}
}

func TestInitExplicitlySelectingAllAdaptersMatchesDefaultOutput(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := filepath.Join(dir, config.DefaultFilename)
	writeInitConfigWithoutStbench(t, configPath)
	originalConfig := readInitFile(t, configPath)

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute default stbench init: %v", err)
	}
	defaultConfig := readInitFile(t, configPath)
	defaultAdapters := readAdapterContents(t, filepath.Join(dir, ".local", "stbench", "adapters"))

	if err := os.RemoveAll(filepath.Join(dir, ".local", "stbench")); err != nil {
		t.Fatalf("remove default managed state: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatalf("remove generated .gitignore: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatalf("restore config: %v", err)
	}

	root = NewRootCommand()
	root.SetArgs([]string{"init", "--adapters=cloud,local,coding-agent"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute explicit-all stbench init: %v", err)
	}
	if got := readInitFile(t, configPath); got != defaultConfig {
		t.Fatalf("explicit-all config differs from default output\ngot:  %q\nwant: %q", got, defaultConfig)
	}
	gotAdapters := readAdapterContents(t, filepath.Join(dir, ".local", "stbench", "adapters"))
	if len(gotAdapters) != len(defaultAdapters) {
		t.Fatalf("explicit-all adapter file count = %d, want default count %d", len(gotAdapters), len(defaultAdapters))
	}
	for name, wantContents := range defaultAdapters {
		gotContents, exists := gotAdapters[name]
		if !exists || !bytes.Equal(gotContents, wantContents) {
			t.Errorf("explicit-all adapter %s differs from default output", name)
		}
	}
}

func TestInitWritesStbenchIntoFlowStyleConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const flowConfig = `{schema: openapi.json, base_url: 'http://localhost:8080', reports_dir: reports, schemathesis: {workers: 1}, campaigns: {baseline: {kind: baseline}, local-candidate: {kind: candidate, agent: local-agent, model: model-name, effort: high, adapter: local}}}
`
	configPath := filepath.Join(dir, config.DefaultFilename)
	if err := os.WriteFile(configPath, []byte(flowConfig), 0o644); err != nil {
		t.Fatalf("write flow-style config: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load updated flow-style config: %v", err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("validate updated flow-style config: %v", err)
	}
}

func TestInitRejectsConfigWithoutMappingRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const scalarConfig = "not-a-mapping\n"
	configPath := filepath.Join(dir, config.DefaultFilename)
	if err := os.WriteFile(configPath, []byte(scalarConfig), 0o644); err != nil {
		t.Fatalf("write scalar config: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	if err == nil {
		t.Fatal("execute stbench init error = nil, want mapping-root error")
	}
	want := "decode stcompare.yaml: top level must be a YAML mapping"
	if err.Error() != want {
		t.Fatalf("execute stbench init error = %q, want %q", err, want)
	}
	if got := readInitFile(t, configPath); got != scalarConfig {
		t.Fatalf("config = %q, want invalid content unchanged", got)
	}
}

func TestInitAddsAdaptersToExistingStbenchConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const existing = `schema: openapi.json
stbench:
  hardware: keep-user-value
`
	configPath := filepath.Join(dir, config.DefaultFilename)
	if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	var output bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	gotConfig := readInitFile(t, configPath)
	if !strings.HasPrefix(gotConfig, existing) {
		t.Fatalf("config = %q, want existing content preserved", gotConfig)
	}
	for _, role := range []string{"cloud:", "local:", "coding-agent:"} {
		if !strings.Contains(gotConfig, role) {
			t.Errorf("config = %q, want %s adapter entry", gotConfig, role)
		}
	}
	if got := output.String(); !strings.Contains(got, "Wrote stbench configuration") {
		t.Fatalf("init output = %q, want configuration-written notice", got)
	}

	harnessDir, harnessErr := managedHarnessDir(dir)
	if harnessErr != nil {
		t.Fatalf("resolve managed harness directory: %v", harnessErr)
	}
	for _, name := range []string{"stop.sh", "reset.sh", "build.sh", "start.sh"} {
		if _, statErr := os.Stat(filepath.Join(harnessDir, name)); statErr != nil {
			t.Errorf("stat %s: %v — scaffold must survive when the stbench block already exists", name, statErr)
		}
	}
	gitignore := readInitFile(t, filepath.Join(dir, ".gitignore"))
	if !strings.Contains(gitignore, ".local/stbench/") {
		t.Fatalf(".gitignore = %q, want .local/stbench/ entry", gitignore)
	}
}

func TestInitDirectsUserToCreateMissingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	if err == nil {
		t.Fatal("execute stbench init error = nil, want missing-config error")
	}
	if got, want := err.Error(), "stcompare.yaml does not exist; run stcompare config init first"; got != want {
		t.Fatalf("execute stbench init error = %q, want %q", got, want)
	}

	harnessDir, harnessErr := managedHarnessDir(dir)
	if harnessErr != nil {
		t.Fatalf("resolve managed harness directory: %v", harnessErr)
	}
	for _, name := range []string{"stop.sh", "reset.sh", "build.sh", "start.sh"} {
		if _, statErr := os.Stat(filepath.Join(harnessDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after missing config, want rollback", name)
		}
	}
	for _, name := range []string{"_protocol.py", "adapter.py", "coding_agent_adapter.py", "local_model_adapter.py"} {
		path := filepath.Join(harnessDir, "adapters", name)
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after missing config, want rollback", name)
		}
	}
}

func TestInitRollsBackScaffoldWhenConfigWriteFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := filepath.Join(dir, config.DefaultFilename)
	writeInitConfigWithoutStbench(t, configPath)
	if err := os.Chmod(configPath, 0o444); err != nil {
		t.Fatalf("make config read-only: %v", err)
	}
	probe, probeErr := os.OpenFile(configPath, os.O_WRONLY, 0)
	if probeErr == nil {
		if err := probe.Close(); err != nil {
			t.Fatalf("close write-permission probe: %v", err)
		}
		t.Skip("file permissions do not prevent writes in this environment")
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	if err == nil {
		t.Fatal("execute stbench init error = nil, want config-write error")
	}
	if !strings.Contains(err.Error(), "write stcompare.yaml") {
		t.Fatalf("execute stbench init error = %q, want config-write error", err)
	}

	harnessDir, harnessErr := managedHarnessDir(dir)
	if harnessErr != nil {
		t.Fatalf("resolve managed harness directory: %v", harnessErr)
	}
	for _, name := range []string{"stop.sh", "reset.sh", "build.sh", "start.sh"} {
		if _, statErr := os.Stat(filepath.Join(harnessDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after config write failure, want rollback", name)
		}
	}
	for _, name := range []string{"_protocol.py", "adapter.py", "coding_agent_adapter.py", "local_model_adapter.py"} {
		path := filepath.Join(harnessDir, "adapters", name)
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after config write failure, want rollback", name)
		}
	}
}

func TestInitPreservesExistingLifecycleFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))
	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("create managed harness directory: %v", err)
	}
	const sentinel = "keep me\n"
	if err := os.WriteFile(filepath.Join(harnessDir, "reset.sh"), []byte(sentinel), 0o755); err != nil {
		t.Fatalf("write existing reset.sh: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	contents, readErr := os.ReadFile(filepath.Join(harnessDir, "reset.sh"))
	if readErr != nil {
		t.Fatalf("read existing reset.sh: %v", readErr)
	}
	if string(contents) != sentinel {
		t.Fatalf("reset.sh = %q, want sentinel", contents)
	}
	for _, name := range []string{"stop.sh", "build.sh", "start.sh"} {
		if _, statErr := os.Stat(filepath.Join(harnessDir, name)); statErr != nil {
			t.Errorf("stat newly installed %s: %v", name, statErr)
		}
	}
}

func TestInitPreservesExistingSelectedAdapterFiles(t *testing.T) {
	tests := []struct {
		role     string
		filename string
	}{
		{role: "cloud", filename: "adapter.py"},
		{role: "coding-agent", filename: "coding_agent_adapter.py"},
		{role: "local", filename: "local_model_adapter.py"},
	}
	for _, test := range tests {
		t.Run(test.role, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))
			harnessDir, err := managedHarnessDir(dir)
			if err != nil {
				t.Fatalf("resolve managed harness directory: %v", err)
			}
			adapterDir := filepath.Join(harnessDir, "adapters")
			if err := os.MkdirAll(adapterDir, 0o755); err != nil {
				t.Fatalf("create adapter directory: %v", err)
			}
			const sentinel = "keep me\n"
			path := filepath.Join(adapterDir, test.filename)
			if err := os.WriteFile(path, []byte(sentinel), 0o644); err != nil {
				t.Fatalf("write existing adapter: %v", err)
			}

			root := NewRootCommand()
			root.SetArgs([]string{"init", "--adapters=" + test.role})
			if err := root.Execute(); err != nil {
				t.Fatalf("execute stbench init: %v", err)
			}
			if got := readInitFile(t, path); got != sentinel {
				t.Fatalf("existing adapter = %q, want sentinel", got)
			}
			if _, err := os.Stat(filepath.Join(adapterDir, "_protocol.py")); err != nil {
				t.Fatalf("stat synchronized protocol: %v", err)
			}
		})
	}
}

func TestInitGitIgnoresInstalledAdapters(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))
	if output, err := exec.Command("git", "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize git repository: %v: %s", err, output)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	adapterPath := filepath.Join(".local", "stbench", "adapters", "adapter.py")
	if output, err := exec.Command("git", "check-ignore", "--quiet", adapterPath).CombinedOutput(); err != nil {
		t.Fatalf("git check-ignore %s: %v: %s", adapterPath, err, output)
	}
}

func TestInitPreservesExistingGitignoreAndDoesNotDuplicateManagedState(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))
	const existing = "reports/\n.local/stbench/\n"
	if err := os.WriteFile(".gitignore", []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing .gitignore: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	contents := readInitFile(t, ".gitignore")
	if contents != existing {
		t.Fatalf(".gitignore = %q, want existing content unchanged", contents)
	}
	if count := strings.Count(contents, ".local/stbench/"); count != 1 {
		t.Fatalf(".gitignore contains .local/stbench/ %d times, want once", count)
	}
}

func TestStbenchConfigBlockPreservesShellQuotingThroughYAML(t *testing.T) {
	harnessDir := filepath.Join(t.TempDir(), "api project state")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("create harness directory: %v", err)
	}
	stopPath := filepath.Join(harnessDir, "stop.sh")
	if err := os.WriteFile(stopPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stop script: %v", err)
	}

	var document struct {
		Stbench config.StbenchConfig `yaml:"stbench"`
	}
	if err := yaml.Unmarshal([]byte(stbenchConfigBlockFor(harnessDir)), &document); err != nil {
		t.Fatalf("parse generated config stanza: %v", err)
	}

	if got, want := document.Stbench.Lifecycle.Stop, quoteShellPath(stopPath); got != want {
		t.Fatalf("stop command = %q, want shell-quoted %q", got, want)
	}
	if err := exec.Command("sh", "-c", document.Stbench.Lifecycle.Stop).Run(); err != nil {
		t.Fatalf("run generated stop command: %v", err)
	}
}

func TestManagedHarnessDirUsesRepositoryLocalStateByDefault(t *testing.T) {
	repositoryDir := t.TempDir()
	t.Setenv(stbenchStateDirEnv, "")

	got, err := managedHarnessDir(repositoryDir)
	if err != nil {
		t.Fatalf("managedHarnessDir() error = %v", err)
	}
	want := filepath.Join(repositoryDir, ".local", "stbench")
	if got != want {
		t.Fatalf("managedHarnessDir() = %q, want %q", got, want)
	}
}

func TestManagedHarnessDirResolvesRelativeStateToRepository(t *testing.T) {
	repositoryDir := t.TempDir()
	t.Setenv(stbenchStateDirEnv, ".local/stbench")

	got, err := managedHarnessDir(repositoryDir)
	if err != nil {
		t.Fatalf("managedHarnessDir() error = %v", err)
	}
	want := filepath.Join(repositoryDir, ".local", "stbench")
	if got != want {
		t.Fatalf("managedHarnessDir() = %q, want %q", got, want)
	}
}

func TestManagedHarnessDirRejectsUnmanagedRepositoryLocalState(t *testing.T) {
	repositoryDir := t.TempDir()
	t.Setenv(stbenchStateDirEnv, ".state")

	_, err := managedHarnessDir(repositoryDir)
	if err == nil || !strings.Contains(err.Error(), "repository-local state must use .local/stbench") {
		t.Fatalf("managedHarnessDir() error = %v, want managed-state error", err)
	}
}

func readInitFile(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func writeInitConfigWithoutStbench(t *testing.T, path string) {
	t.Helper()

	const contents = `schema: openapi.json
base_url: http://localhost:8080
reports_dir: reports
schemathesis:
  workers: 1
campaigns:
  baseline:
    kind: baseline
  local-candidate:
    kind: candidate
    agent: local-agent
    model: model-name
    effort: high
    adapter: local
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config without stbench: %v", err)
	}
}

func executeInitInTempRepository(t *testing.T, args ...string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)
	writeInitConfigWithoutStbench(t, filepath.Join(dir, config.DefaultFilename))

	root := NewRootCommand()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute stbench init: %v", err)
	}

	harnessDir, err := managedHarnessDir(dir)
	if err != nil {
		t.Fatalf("resolve managed harness directory: %v", err)
	}
	return dir, harnessDir
}

func assertInstalledAdapterNames(t *testing.T, directory string, want []string) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read installed adapter directory: %v", err)
	}
	got := make(map[string]bool, len(entries))
	for _, entry := range entries {
		got[entry.Name()] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, name := range want {
		wantSet[name] = true
	}
	if !maps.Equal(got, wantSet) {
		t.Fatalf("installed adapter files = %#v, want %#v", got, wantSet)
	}
}

type fakeTerminalStream struct {
	fd uintptr
}

func (stream fakeTerminalStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (stream fakeTerminalStream) Write(contents []byte) (int, error) {
	return len(contents), nil
}

func (stream fakeTerminalStream) Fd() uintptr {
	return stream.fd
}

func readAdapterContents(t *testing.T, directory string) map[string][]byte {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read adapter directory: %v", err)
	}
	contents := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read adapter %s: %v", entry.Name(), err)
		}
		contents[entry.Name()] = data
	}
	return contents
}
