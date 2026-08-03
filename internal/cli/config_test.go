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

type configDocument = map[string]any

type configCommandOutcome struct {
	Error  string
	Output string
}

func loadDefaultConfig(t *testing.T) configDocument {
	t.Helper()

	contents, err := os.ReadFile("testdata/default-config.yaml")
	if err != nil {
		t.Fatalf("read default config: %v", err)
	}

	return decodeConfig(t, contents)
}

func decodeConfig(t *testing.T, contents []byte) configDocument {
	t.Helper()

	var document configDocument
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	return document
}

func cloneConfig(t *testing.T, document configDocument) configDocument {
	t.Helper()

	contents, err := yaml.Marshal(document)
	if err != nil {
		t.Fatalf("encode config clone: %v", err)
	}

	return decodeConfig(t, contents)
}

func writeConfig(t *testing.T, path string, document configDocument) {
	t.Helper()

	contents, err := yaml.Marshal(document)
	if err != nil {
		t.Fatalf("encode input config: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write input config: %v", err)
	}
}

func configSection(t *testing.T, document configDocument, name string) map[string]any {
	t.Helper()

	section, ok := document[name].(map[string]any)
	if !ok {
		t.Fatalf("config section %q = %#v, want mapping", name, document[name])
	}

	return section
}

func assertConfigShowRejected(t *testing.T, mutate func(configDocument), wantError string) {
	t.Helper()

	document := loadDefaultConfig(t)
	mutate(document)
	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", document)

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "show"})
	err := root.Execute()

	got := configCommandOutcome{Output: output.String()}
	if err != nil {
		got.Error = err.Error()
	}
	want := configCommandOutcome{Error: wantError}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config show outcome = %#v, want %#v", got, want)
	}
}

func TestConfigInitWritesDefaultConfig(t *testing.T) {
	want := loadDefaultConfig(t)
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
	got := decodeConfig(t, contents)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded default config = %#v, want %#v", got, want)
	}
}

func TestConfigShowPrintsEffectiveConfig(t *testing.T) {
	input := loadDefaultConfig(t)
	input["schema"] = "fixture-openapi.yaml"
	input["base_url"] = "http://fixture.invalid:9000"
	input["reports_dir"] = "fixture-reports"
	schemathesis := configSection(t, input, "schemathesis")
	schemathesis["seed"] = 7
	schemathesis["workers"] = 2
	schemathesis["generation_deterministic"] = false
	schemathesis["generation_database"] = "fixture-db"
	schemathesis["reports"] = []any{"junit", "har"}
	schemathesis["output_sanitize"] = true
	schemathesis["output_truncate"] = true
	schemathesis["extra_args"] = []any{"--checks", "all"}
	input["campaigns"] = map[string]any{
		"reference":  map[string]any{"kind": "baseline"},
		"challenger": map[string]any{"kind": "candidate"},
	}

	want := cloneConfig(t, input)
	want["schema"] = "overridden-openapi.json"
	want["base_url"] = "http://localhost:9090"
	want["reports_dir"] = "effective-reports"
	wantSchemathesis := configSection(t, want, "schemathesis")
	wantSchemathesis["seed"] = 4242
	wantSchemathesis["workers"] = 8

	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", input)

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
	got := decodeConfig(t, output.Bytes())

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective config = %#v, want %#v", got, want)
	}
}

func TestConfigShowRejectsMissingSchemaBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		delete(document, "schema")
	}, "schema is required")
}

func TestConfigShowLoadsExplicitConfigPath(t *testing.T) {
	want := loadDefaultConfig(t)
	want["schema"] = "api/explicit-openapi.yaml"
	want["base_url"] = "https://explicit.example.test"
	want["reports_dir"] = "explicit-reports"
	configSection(t, want, "schemathesis")["seed"] = 9876
	want["campaigns"] = map[string]any{
		"stable":     map[string]any{"kind": "baseline"},
		"experiment": map[string]any{"kind": "candidate"},
	}
	configPath := filepath.Join(t.TempDir(), "custom.yaml")
	writeConfig(t, configPath, want)
	t.Chdir(t.TempDir())

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"--config", configPath, "config", "show"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config show: %v", err)
	}
	got := decodeConfig(t, output.Bytes())

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit config output = %#v, want %#v", got, want)
	}
}

func TestConfigInitWritesToExplicitConfigPath(t *testing.T) {
	want := loadDefaultConfig(t)
	configPath := filepath.Join(t.TempDir(), "custom.yaml")
	t.Chdir(t.TempDir())

	root := cli.NewRootCommand()
	root.SetArgs([]string{"--config", configPath, "config", "init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config init: %v", err)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read explicit config: %v", err)
	}
	got := decodeConfig(t, contents)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit config = %#v, want %#v", got, want)
	}
}

func TestConfigInitRefusesToOverwriteExistingConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	const sentinel = "sentinel: keep\n"
	if err := os.WriteFile("stcompare.yaml", []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	root := cli.NewRootCommand()
	root.SetArgs([]string{"config", "init"})
	err := root.Execute()

	contents, readErr := os.ReadFile("stcompare.yaml")
	if readErr != nil {
		t.Fatalf("read existing config: %v", readErr)
	}
	got := struct {
		Error    string
		Contents string
	}{Contents: string(contents)}
	if err != nil {
		got.Error = err.Error()
	}
	want := struct {
		Error    string
		Contents string
	}{Error: "stcompare.yaml already exists", Contents: sentinel}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config init outcome = %#v, want %#v", got, want)
	}
}

func TestConfigInitForceOverwritesExistingConfig(t *testing.T) {
	want := loadDefaultConfig(t)
	t.Chdir(t.TempDir())
	if err := os.WriteFile("stcompare.yaml", []byte("sentinel: replace\n"), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	root := cli.NewRootCommand()
	root.SetArgs([]string{"config", "init", "--force"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute forced config init: %v", err)
	}

	contents, err := os.ReadFile("stcompare.yaml")
	if err != nil {
		t.Fatalf("read forced config: %v", err)
	}
	got := decodeConfig(t, contents)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forced config = %#v, want %#v", got, want)
	}
}

func TestConfigShowAppliesOmittedSchemathesisDefaults(t *testing.T) {
	input := loadDefaultConfig(t)
	input["schemathesis"] = map[string]any{"seed": 777}
	input["campaigns"] = map[string]any{
		"reference":  map[string]any{"kind": "baseline"},
		"experiment": map[string]any{"kind": "candidate"},
	}
	want := loadDefaultConfig(t)
	configSection(t, want, "schemathesis")["seed"] = 777
	want["campaigns"] = cloneConfig(t, input)["campaigns"]

	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", input)

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "show"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config show: %v", err)
	}
	got := decodeConfig(t, output.Bytes())

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partial config output = %#v, want %#v", got, want)
	}
}

func TestConfigShowAppliesOmittedComparisonDefaults(t *testing.T) {
	input := loadDefaultConfig(t)
	delete(input, "comparison")
	want := cloneConfig(t, input)
	want["comparison"] = map[string]any{
		"missing_resource_statuses": []any{404, 410},
		"precondition_heuristics":   []any{},
		"normalization": map[string]any{
			"default_rules": true,
			"body_fields":   []any{},
			"headers":       []any{},
		},
	}

	t.Chdir(t.TempDir())
	writeConfig(t, "stcompare.yaml", input)

	var output bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "show"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute config show: %v", err)
	}
	got := decodeConfig(t, output.Bytes())

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partial config output = %#v, want %#v", got, want)
	}
}

func TestConfigShowRejectsNonAbsoluteBaseURLBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		document["base_url"] = "localhost:8080"
	}, "base_url must be an absolute HTTP(S) URL")
}

func TestConfigShowRejectsStbenchHealthURLHostPortMismatchBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		document["stbench"] = map[string]any{
			"lifecycle": map[string]any{
				"health_url": "http://localhost:9090/health",
			},
		}
	}, `stbench.lifecycle.health_url host/port must match base_url: got "localhost:9090", want "localhost:8080"`)
}

func TestConfigShowRejectsMissingReportsDirBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		delete(document, "reports_dir")
	}, "reports_dir is required")
}

func TestConfigShowRejectsWorkersBelowOneBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		configSection(t, document, "schemathesis")["workers"] = 0
	}, "schemathesis.workers must be at least 1")
}

func TestConfigShowRejectsInvalidPreconditionHeuristicPathPatternBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		document["comparison"] = map[string]any{
			"missing_resource_statuses": []any{404, 410},
			"precondition_heuristics": []any{
				map[string]any{
					"name":         "generated-widget",
					"method":       "GET",
					"path_pattern": "[",
				},
			},
		}
	}, "comparison.precondition_heuristics[0].path_pattern must be a valid regular expression")
}

func TestConfigShowRejectsSemanticallyInvalidComparisonBeforeOutput(t *testing.T) {
	tests := []struct {
		name       string
		comparison configDocument
		wantError  string
	}{
		{
			name: "unsupported missing status",
			comparison: configDocument{
				"missing_resource_statuses": []any{500},
				"precondition_heuristics":   []any{},
			},
			wantError: "comparison.missing_resource_statuses[0] must be one of 401, 403, 404, or 410",
		},
		{
			name: "blank heuristic name",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"precondition_heuristics": []any{
					map[string]any{
						"name":         "   ",
						"method":       "GET",
						"path_pattern": `^/widgets/[0-9a-f]+$`,
					},
				},
			},
			wantError: "comparison.precondition_heuristics[0].name is required",
		},
		{
			name: "duplicate heuristic name",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"precondition_heuristics": []any{
					map[string]any{
						"name":         "generated-widget",
						"method":       "GET",
						"path_pattern": `^/widgets/[0-9a-f]+$`,
					},
					map[string]any{
						"name":         "generated-widget",
						"method":       "POST",
						"path_pattern": `^/widgets$`,
					},
				},
			},
			wantError: "comparison.precondition_heuristics[1].name must be unique",
		},
		{
			name: "blank heuristic method",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"precondition_heuristics": []any{
					map[string]any{
						"name":         "generated-widget",
						"method":       "   ",
						"path_pattern": `^/widgets/[0-9a-f]+$`,
					},
				},
			},
			wantError: "comparison.precondition_heuristics[0].method is required",
		},
		{
			name: "empty path pattern",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"precondition_heuristics": []any{
					map[string]any{
						"name":         "generated-widget",
						"method":       "GET",
						"path_pattern": "",
					},
				},
			},
			wantError: "comparison.precondition_heuristics[0].path_pattern is required",
		},
		{
			name: "blank normalization body rule field",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"precondition_heuristics":   []any{},
				"normalization": map[string]any{
					"default_rules": true,
					"body_fields": []any{
						map[string]any{"name": "volatile-id", "field_name": " "},
					},
					"headers": []any{},
				},
			},
			wantError: "comparison.normalization.body_fields[0].field_name is required",
		},
		{
			name: "blank normalization header rule name",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"precondition_heuristics":   []any{},
				"normalization": map[string]any{
					"default_rules": true,
					"body_fields":   []any{},
					"headers": []any{
						map[string]any{"name": " ", "header_name": "date"},
					},
				},
			},
			wantError: "comparison.normalization.headers[0].name is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertConfigShowRejected(t, func(document configDocument) {
				document["comparison"] = test.comparison
			}, test.wantError)
		})
	}
}

func TestConfigShowAcceptsComparisonValidationBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		comparison configDocument
	}{
		{
			name: "empty missing resource statuses",
			comparison: configDocument{
				"missing_resource_statuses": []any{},
				"normalization": map[string]any{
					"default_rules": true,
					"body_fields":   []any{},
					"headers":       []any{},
				},
				"precondition_heuristics": []any{
					map[string]any{
						"name":         "generated-widget",
						"method":       "GET",
						"path_pattern": `^/widgets/[0-9a-f]+$`,
					},
				},
			},
		},
		{
			name: "lowercase heuristic method",
			comparison: configDocument{
				"missing_resource_statuses": []any{404, 410},
				"normalization": map[string]any{
					"default_rules": true,
					"body_fields":   []any{},
					"headers":       []any{},
				},
				"precondition_heuristics": []any{
					map[string]any{
						"name":         "generated-widget",
						"method":       "get",
						"path_pattern": `^/widgets/[0-9a-f]+$`,
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := loadDefaultConfig(t)
			want["comparison"] = test.comparison
			t.Chdir(t.TempDir())
			writeConfig(t, "stcompare.yaml", want)

			var output bytes.Buffer
			root := cli.NewRootCommand()
			root.SetOut(&output)
			root.SetArgs([]string{"config", "show"})
			if err := root.Execute(); err != nil {
				t.Fatalf("execute config show: %v", err)
			}
			got := decodeConfig(t, output.Bytes())

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("effective config = %#v, want %#v", got, want)
			}
		})
	}
}

func TestConfigShowRejectsMissingCampaignsBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		delete(document, "campaigns")
	}, "at least one campaign is required")
}

func TestConfigShowRejectsInvalidCampaignKindBeforeOutput(t *testing.T) {
	assertConfigShowRejected(t, func(document configDocument) {
		document["campaigns"] = map[string]any{
			"experiment": map[string]any{"kind": "control"},
		}
	}, "campaign \"experiment\" has invalid kind \"control\": must be baseline or candidate")
}
