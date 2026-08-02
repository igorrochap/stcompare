package bench

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	for _, name := range []string{"stop.sh", "reset.sh", "build.sh", "start.sh"} {
		path := filepath.Join(dir, name)
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
	}

	stopScript := readInitFile(t, "stop.sh")
	if !strings.Contains(stopScript, "stop must no-op cleanly when nothing is running") {
		t.Errorf("stop.sh = %q, want no-process safety comment", stopScript)
	}
	if err := exec.Command("sh", filepath.Join(dir, "stop.sh")).Run(); err != nil {
		t.Errorf("first stop.sh run: %v", err)
	}
	if err := exec.Command("sh", filepath.Join(dir, "stop.sh")).Run(); err != nil {
		t.Errorf("second stop.sh run: %v", err)
	}

	resetScript := readInitFile(t, "reset.sh")
	if !strings.Contains(resetScript, "reset must not revert source changes") {
		t.Errorf("reset.sh = %q, want source-preservation warning", resetScript)
	}

	configStanza := output.String()
	for _, want := range []string{
		"# Add this stanza to stcompare.yaml",
		"stbench:",
		"stop: ./stop.sh",
		"reset: ./reset.sh",
		"build: ./build.sh",
		"start: ./start.sh",
	} {
		if !strings.Contains(configStanza, want) {
			t.Errorf("init output = %q, want %q", configStanza, want)
		}
	}
}

func TestInitRefusesToOverwriteScaffoldFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const sentinel = "keep me\n"
	if err := os.WriteFile("reset.sh", []byte(sentinel), 0o755); err != nil {
		t.Fatalf("write existing reset.sh: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	if err == nil {
		t.Fatal("execute stbench init error = nil, want existing-file error")
	}
	if !strings.Contains(err.Error(), "reset.sh already exists") {
		t.Fatalf("execute stbench init error = %q, want reset.sh conflict", err)
	}

	contents, readErr := os.ReadFile("reset.sh")
	if readErr != nil {
		t.Fatalf("read existing reset.sh: %v", readErr)
	}
	if string(contents) != sentinel {
		t.Fatalf("reset.sh = %q, want sentinel", contents)
	}
	for _, name := range []string{"stop.sh", "build.sh", "start.sh"} {
		if _, statErr := os.Stat(name); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after failed init, want rollback", name)
		}
	}
}

func readInitFile(t *testing.T, name string) string {
	t.Helper()

	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}
