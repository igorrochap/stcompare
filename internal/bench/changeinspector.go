package bench

import (
	"bufio"
	"bytes"
	"os/exec"
	"strings"
)

// GitChangeInspector reports candidate source files an agent has modified,
// using the git working tree. Because the reset hook deliberately preserves
// source edits, `git status` reflects exactly the agent's cumulative progress.
type GitChangeInspector struct {
	// WorkingDir is the candidate source directory (a git worktree).
	WorkingDir string
}

var _ ChangeInspector = (*GitChangeInspector)(nil)

// NewGitChangeInspector creates an inspector rooted at the candidate source dir.
func NewGitChangeInspector(workingDir string) *GitChangeInspector {
	return &GitChangeInspector{WorkingDir: workingDir}
}

// Changed returns modified, added, and untracked paths relative to the worktree
// root. A non-git directory (or any git failure) yields a nil slice and error;
// callers treat that as "no file detail available" and narrate elapsed only.
func (inspector *GitChangeInspector) Changed() ([]string, error) {
	command := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	command.Dir = inspector.WorkingDir
	var stdout bytes.Buffer
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		return nil, err
	}
	return parsePorcelainPaths(stdout.Bytes()), nil
}

// parsePorcelainPaths extracts paths from `git status --porcelain` output. Each
// line is "XY <path>"; rename lines carry "orig -> new" and we keep the new
// path.
func parsePorcelainPaths(output []byte) []string {
	var paths []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) <= 3 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if arrow := strings.Index(path, " -> "); arrow >= 0 {
			path = path[arrow+len(" -> "):]
		}
		paths = append(paths, strings.Trim(path, "\""))
	}
	return paths
}
