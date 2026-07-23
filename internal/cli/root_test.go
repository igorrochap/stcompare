package cli_test

import (
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"stcompare/internal/cli"
)

type configDocument struct {
	Schema       string                 `yaml:"schema"`
	BaseURL      string                 `yaml:"base_url"`
	ReportsDir   string                 `yaml:"reports_dir"`
	Schemathesis schemathesisDocument   `yaml:"schemathesis"`
	Campaigns    map[string]campaignDoc `yaml:"campaigns"`
}

type schemathesisDocument struct {
	Seed                    int      `yaml:"seed"`
	Workers                 int      `yaml:"workers"`
	GenerationDeterministic bool     `yaml:"generation_deterministic"`
	GenerationDatabase      string   `yaml:"generation_database"`
	Reports                 []string `yaml:"reports"`
	OutputSanitize          bool     `yaml:"output_sanitize"`
	OutputTruncate          bool     `yaml:"output_truncate"`
	ExtraArgs               []string `yaml:"extra_args"`
}

type campaignDoc struct {
	Kind string `yaml:"kind"`
}

func TestConfigInitWritesDefaultConfig(t *testing.T) {
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

	var got configDocument
	if err := yaml.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode stcompare.yaml: %v", err)
	}

	want := configDocument{
		Schema:     "openapi.json",
		BaseURL:    "http://localhost:8080",
		ReportsDir: "reports",
		Schemathesis: schemathesisDocument{
			Seed:                    12345,
			Workers:                 1,
			GenerationDeterministic: true,
			GenerationDatabase:      "none",
			Reports:                 []string{"junit", "vcr", "har", "ndjson"},
			OutputSanitize:          false,
			OutputTruncate:          false,
			ExtraArgs:               []string{},
		},
		Campaigns: map[string]campaignDoc{
			"baseline": {Kind: "baseline"},
			"gpt5.6":   {Kind: "candidate"},
			"sonnet5":  {Kind: "candidate"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded default config = %#v, want %#v", got, want)
	}
}
