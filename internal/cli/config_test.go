package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"stcompare/internal/cli"
)

func TestConfigInitWritesDefaultConfig(t *testing.T) {
	wantContents, err := os.ReadFile("testdata/default-config.yaml")
	if err != nil {
		t.Fatalf("read expected config: %v", err)
	}

	var want any
	if err := yaml.Unmarshal(wantContents, &want); err != nil {
		t.Fatalf("decode expected config: %v", err)
	}

	t.Chdir(t.TempDir())

	root := cli.NewRootCommand()
	root.SetArgs([]string{"config", "init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config init: %v", err)
	}

	contents, err := os.ReadFile("stcompare.yaml")
	if err != nil {
		t.Fatalf("read stcompare.yaml: %v", err)
	}

	var got any
	if err := yaml.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode stcompare.yaml: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded default config = %#v, want %#v", got, want)
	}
}

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

func TestConfigShowRejectsMissingSchemaBeforeOutput(t *testing.T) {
	fixture, err := os.ReadFile("testdata/missing-schema.yaml")
	if err != nil {
		t.Fatalf("read missing-schema config: %v", err)
	}

	t.Chdir(t.TempDir())
	if err := os.WriteFile("stcompare.yaml", fixture, 0o644); err != nil {
		t.Fatalf("write input config: %v", err)
	}

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "show"})
	err = root.Execute()

	got := struct {
		Error  string
		Output string
	}{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error  string
		Output string
	}{Error: "schema is required"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config show outcome = %#v, want %#v", got, want)
	}
}

func TestConfigShowLoadsExplicitConfigPath(t *testing.T) {
	fixture, err := os.ReadFile("testdata/explicit-path.yaml")
	if err != nil {
		t.Fatalf("read explicit-path config: %v", err)
	}

	var want any
	if err := yaml.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("decode expected config: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(configPath, fixture, 0o644); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}
	t.Chdir(t.TempDir())

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"--config", configPath, "config", "show"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config show: %v", err)
	}

	var got any
	if err := yaml.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode explicit config output: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit config output = %#v, want %#v", got, want)
	}
}
