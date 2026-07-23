package cli_test

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"stcompare/internal/cli"
)

func TestConfigShowPrintsEffectiveConfig(t *testing.T) {
	input, err := os.ReadFile("testdata/show-input.yaml")
	if err != nil {
		t.Fatalf("read input config: %v", err)
	}

	expected, err := os.ReadFile("testdata/show-expected.yaml")
	if err != nil {
		t.Fatalf("read expected config: %v", err)
	}

	var want any
	if err := yaml.Unmarshal(expected, &want); err != nil {
		t.Fatalf("decode expected config: %v", err)
	}

	t.Chdir(t.TempDir())
	if err := os.WriteFile("stcompare.yaml", input, 0o644); err != nil {
		t.Fatalf("write input config: %v", err)
	}

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{
		"config", "show",
		"--schema", "overridden-openapi.json",
		"--base-url", "http://localhost:9090",
		"--reports-dir", "effective-reports",
		"--seed", "4242",
		"--workers", "8",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config show: %v", err)
	}

	var got any
	if err := yaml.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode effective config: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective config = %#v, want %#v", got, want)
	}
}
