package bench

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"stcompare/internal/config"
)

func TestInitCreatesRunnableLifecycleScaffoldAndConfigStanza(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

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

	configStanza := output.String()
	for _, want := range []string{
		"# Add this stanza to stcompare.yaml",
		"stbench:",
		"campaign: gpt5.6",
		"source_dir: .",
		filepath.Join(harnessDir, "stop.sh"),
		filepath.Join(harnessDir, "reset.sh"),
		filepath.Join(harnessDir, "build.sh"),
		filepath.Join(harnessDir, "start.sh"),
	} {
		if !strings.Contains(configStanza, want) {
			t.Errorf("init output = %q, want %q", configStanza, want)
		}
	}
	if strings.Contains(configStanza, "candidate_dir:") || strings.Contains(configStanza, "candidate: gpt5.6") {
		t.Fatalf("init output = %q, want campaign/source_dir names", configStanza)
	}
}

func TestInitRefusesToOverwriteScaffoldFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
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
	err = root.Execute()
	if err == nil {
		t.Fatal("execute stbench init error = nil, want existing-file error")
	}
	if !strings.Contains(err.Error(), "reset.sh already exists") {
		t.Fatalf("execute stbench init error = %q, want reset.sh conflict", err)
	}

	contents, readErr := os.ReadFile(filepath.Join(harnessDir, "reset.sh"))
	if readErr != nil {
		t.Fatalf("read existing reset.sh: %v", readErr)
	}
	if string(contents) != sentinel {
		t.Fatalf("reset.sh = %q, want sentinel", contents)
	}
	for _, name := range []string{"stop.sh", "build.sh", "start.sh"} {
		if _, statErr := os.Stat(filepath.Join(harnessDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after failed init, want rollback", name)
		}
	}
}

func TestInitPreservesExistingGitignoreAndDoesNotDuplicateManagedState(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
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

func TestLifecycleConfigStanzaPreservesShellQuotingThroughYAML(t *testing.T) {
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
	if err := yaml.Unmarshal([]byte(lifecycleConfigStanzaFor(harnessDir)), &document); err != nil {
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
