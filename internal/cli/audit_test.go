package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stcompare/internal/audit"
)

func TestAuditRenderCommandWritesReportWithoutComparisonArtifact(t *testing.T) {
	directory := t.TempDir()
	auditPath := filepath.Join(directory, "benchmark-audit.json")
	outputPath := filepath.Join(directory, "benchmark-audit.html")
	contents, err := json.Marshal(audit.Artifact{
		SchemaVersion: audit.SchemaVersion,
		Run:           audit.Run{ID: "run-1", Agent: "local-model", Model: "model"},
		Capture:       audit.Capture{Enabled: true, Status: "complete", Complete: true},
	})
	if err != nil {
		t.Fatalf("marshal audit fixture: %v", err)
	}
	if err := os.WriteFile(auditPath, contents, 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"audit", "render", "--audit", auditPath, "--out", outputPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("render audit command: %v", err)
	}
	report, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read audit report: %v", err)
	}
	if !strings.Contains(string(report), "Model-turn audit") {
		t.Fatalf("audit report missing title: %s", report)
	}
}

func TestValidateAuditRenderOptionsRequiresBothPaths(t *testing.T) {
	for name, options := range map[string]auditRenderOptions{
		"audit":  {outputPath: "report.html"},
		"output": {auditPath: "audit.json"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAuditRenderOptions(options); err == nil {
				t.Fatal("validate audit options succeeded, want missing path error")
			}
		})
	}
}
