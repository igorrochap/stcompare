package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEntrypointDelegatesToRootCommand(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "stcompare")
	build := exec.Command("go", "build", "-o", binary, "./cmd/stcompare")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stcompare: %v\n%s", err, output)
	}

	runDir := t.TempDir()
	run := exec.Command(binary, "config", "init")
	run.Dir = runDir
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("run stcompare config init: %v\n%s", err, output)
	}

	if _, err := os.Stat(filepath.Join(runDir, "stcompare.yaml")); err != nil {
		t.Fatalf("stcompare.yaml was not created: %v", err)
	}
}
