package cli_test

import (
	"os"
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
