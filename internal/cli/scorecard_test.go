package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stcompare/internal/cli"
)

func TestScorecardBuildWritesJoinedHTMLReport(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "comparison.json")
	recordPath := filepath.Join(directory, "benchmark-record.json")
	outputPath := filepath.Join(directory, "scorecard.html")
	writeScorecardFixture(t, comparisonPath, scorecardComparisonJSON)
	writeScorecardFixture(t, recordPath, scorecardRecordJSON)

	root := cli.NewRootCommand()
	root.SetArgs([]string{
		"scorecard", "build",
		"--comparison", comparisonPath,
		"--record", recordPath,
		"--out", outputPath,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("build scorecard: %v", err)
	}

	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read scorecard: %v", err)
	}
	for _, fragment := range []string{"Baseline problem breakdown", "Benchmark Run", "4m 32s"} {
		if !strings.Contains(string(contents), fragment) {
			t.Fatalf("scorecard missing %q:\n%s", fragment, contents)
		}
	}
}

func TestScorecardBuildRequiresBenchmarkRecord(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "comparison.json")
	outputPath := filepath.Join(directory, "scorecard.html")
	writeScorecardFixture(t, comparisonPath, scorecardComparisonJSON)

	root := cli.NewRootCommand()
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"scorecard", "build",
		"--comparison", comparisonPath,
		"--out", outputPath,
	})
	err := root.Execute()

	if err == nil || !strings.Contains(err.Error(), "--record is required to build a scorecard") {
		t.Fatalf("scorecard error = %v, want required record guidance", err)
	}
	assertScorecardDoesNotExist(t, outputPath)
}

func TestScorecardBuildRequiresComparisonAndOutputPaths(t *testing.T) {
	directory := t.TempDir()
	comparisonPath := filepath.Join(directory, "comparison.json")
	recordPath := filepath.Join(directory, "benchmark-record.json")
	outputPath := filepath.Join(directory, "scorecard.html")
	writeScorecardFixture(t, comparisonPath, scorecardComparisonJSON)
	writeScorecardFixture(t, recordPath, scorecardRecordJSON)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "comparison",
			args: []string{"scorecard", "build", "--record", recordPath, "--out", outputPath},
			want: "--comparison is required",
		},
		{
			name: "output",
			args: []string{
				"scorecard", "build",
				"--comparison", comparisonPath,
				"--record", recordPath,
			},
			want: "--out is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := cli.NewRootCommand()
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(test.args)
			err := root.Execute()

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("scorecard error = %v, want %q", err, test.want)
			}
			assertScorecardDoesNotExist(t, outputPath)
		})
	}
}

func TestScorecardBuildRejectsUnreadableOrMalformedInputsWithoutOutput(t *testing.T) {
	tests := []struct {
		name              string
		comparisonFixture string
		recordFixture     string
		comparisonMissing bool
		recordMissing     bool
		want              []string
	}{
		{
			name:              "missing comparison",
			recordFixture:     scorecardRecordJSON,
			comparisonMissing: true,
			want:              []string{"read comparison file", "missing-comparison.json", "no such file or directory"},
		},
		{
			name:              "malformed comparison",
			comparisonFixture: `{not-json}`,
			recordFixture:     scorecardRecordJSON,
			want:              []string{"parse comparison file", "comparison.json", "invalid character"},
		},
		{
			name:              "missing benchmark record",
			comparisonFixture: scorecardComparisonJSON,
			recordMissing:     true,
			want:              []string{"read benchmark record file", "missing-record.json", "no such file or directory"},
		},
		{
			name:              "malformed benchmark record",
			comparisonFixture: scorecardComparisonJSON,
			recordFixture:     `{not-json}`,
			want:              []string{"parse benchmark record file", "benchmark-record.json", "invalid character"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			comparisonPath := filepath.Join(directory, "comparison.json")
			if test.comparisonMissing {
				comparisonPath = filepath.Join(directory, "missing-comparison.json")
			} else {
				writeScorecardFixture(t, comparisonPath, test.comparisonFixture)
			}
			recordPath := filepath.Join(directory, "benchmark-record.json")
			if test.recordMissing {
				recordPath = filepath.Join(directory, "missing-record.json")
			} else {
				writeScorecardFixture(t, recordPath, test.recordFixture)
			}
			outputPath := filepath.Join(directory, "scorecard.html")

			root := cli.NewRootCommand()
			root.SetErr(&bytes.Buffer{})
			root.SetArgs([]string{
				"scorecard", "build",
				"--comparison", comparisonPath,
				"--record", recordPath,
				"--out", outputPath,
			})
			err := root.Execute()

			if err == nil {
				t.Fatal("scorecard build succeeded, want input error")
			}
			for _, fragment := range test.want {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("scorecard error %q missing %q", err, fragment)
				}
			}
			assertScorecardDoesNotExist(t, outputPath)
		})
	}
}

func assertScorecardDoesNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("scorecard output exists or could not be checked: %v", err)
	}
}

func writeScorecardFixture(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}

const scorecardComparisonJSON = `{
	"schema_version": "11",
	"baseline": {"campaign": "baseline"},
	"candidate": {"campaign": "candidate", "base_url": "http://candidate.test"},
	"baseline_problems_available": true,
	"summary": {
		"baseline_problems": {
			"total": 1,
			"evaluable": 1,
			"fixed": 1,
			"fix_rate": {
				"available": true,
				"fixed": 1,
				"denominator": 1,
				"percentage": 100,
				"meaning": "fixture meaning"
			}
		},
		"traffic": {"total": 1, "success_unchanged": 1}
	},
	"problems": []
}`

const scorecardRecordJSON = `{
	"schema_version": "1",
	"agent": "codex",
	"model": "gpt-5.6",
	"iterations": 2,
	"time_ms": {"total": 272000, "agent_fix": 65000},
	"tokens": {"input": 1200, "output": 345, "total": 1545}
}`
