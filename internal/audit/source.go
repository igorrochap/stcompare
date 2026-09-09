package audit

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// SourceStatusComplete identifies a fully captured source snapshot.
	SourceStatusComplete = "complete"
	// SourceStatusPartial identifies a snapshot that could not read every source file.
	SourceStatusPartial = "partial"
	// SourceStatusUnavailable identifies a snapshot that could not establish a source state.
	SourceStatusUnavailable = "unavailable"

	// ChangeOriginModel identifies a net change supported by model modification history.
	ChangeOriginModel = "model"
	// ChangeOriginLifecycle identifies a net change observed around a lifecycle command.
	ChangeOriginLifecycle = "lifecycle"
	// ChangeOriginModelAndLifecycle identifies a change supported by both histories.
	ChangeOriginModelAndLifecycle = "model_and_lifecycle"
	// ChangeOriginUnattributed identifies a change with no established origin.
	ChangeOriginUnattributed = "unattributed"
)

// SourceSnapshot is the source state captured at one benchmark boundary.
type SourceSnapshot struct {
	Status     string       `json:"status"`
	CapturedAt string       `json:"captured_at,omitempty"`
	Files      []SourceFile `json:"files,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// SourceFile is one UTF-8 source file in a snapshot.
type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// SourceChange is one observed lifecycle change or net final source diff.
type SourceChange struct {
	Sequence int     `json:"sequence"`
	ID       string  `json:"id"`
	Path     string  `json:"path"`
	Phase    string  `json:"phase,omitempty"`
	Origin   string  `json:"origin"`
	Before   *string `json:"before"`
	After    *string `json:"after"`
	Diff     string  `json:"diff"`
	Created  bool    `json:"created,omitempty"`
	Deleted  bool    `json:"deleted,omitempty"`
}

// FinalSource contains the actual starting source, final source, and their net diff.
type FinalSource struct {
	Status            string         `json:"status"`
	Starting          SourceSnapshot `json:"starting"`
	Final             SourceSnapshot `json:"final"`
	Diffs             []SourceChange `json:"diffs,omitempty"`
	FilesChangedAtEnd int            `json:"files_changed_at_end"`
}

// ComparisonOutcome is one chronological comparison returned by the runner.
type ComparisonOutcome struct {
	Sequence    int             `json:"sequence"`
	ID          string          `json:"id"`
	RunID       string          `json:"run_id"`
	IterationID string          `json:"iteration_id"`
	Iteration   int             `json:"iteration"`
	Status      string          `json:"status"`
	StartedAt   string          `json:"started_at,omitempty"`
	EndedAt     string          `json:"ended_at,omitempty"`
	DurationMS  int64           `json:"duration_ms,omitempty"`
	ExitCode    int             `json:"exit_code"`
	View        json.RawMessage `json:"view,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// EditSequence joins a model problem input to the next comparison in chronology.
// It deliberately does not claim that the edit caused that comparison outcome.
type EditSequence struct {
	Sequence               int    `json:"sequence"`
	ID                     string `json:"id"`
	RunID                  string `json:"run_id"`
	IterationID            string `json:"iteration_id"`
	Iteration              int    `json:"iteration"`
	ProblemInput           string `json:"problem_input"`
	ComparisonBeforeID     string `json:"comparison_before_id"`
	SubsequentComparisonID string `json:"subsequent_comparison_id,omitempty"`
	EvaluationStatus       string `json:"evaluation_status"`
}

// Evidence is runner-owned evidence appended to an adapter-owned audit artifact.
type Evidence struct {
	FinalSource        FinalSource         `json:"final_source"`
	LifecycleChanges   []SourceChange      `json:"lifecycle_changes,omitempty"`
	ComparisonOutcomes []ComparisonOutcome `json:"comparison_outcomes,omitempty"`
	EditSequences      []EditSequence      `json:"edit_sequences,omitempty"`
}

// CaptureSource records the source files visible below root at a boundary.
// Managed harness state and explicitly excluded outputs are not source.
func CaptureSource(root string, excludes []string) SourceSnapshot {
	snapshot := SourceSnapshot{
		Status:     SourceStatusComplete,
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:      []SourceFile{},
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return unavailableSnapshot(snapshot, fmt.Sprintf("resolve source root: %v", err))
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return unavailableSnapshot(snapshot, fmt.Sprintf("inspect source root: %v", err))
	}
	if !info.IsDir() {
		return unavailableSnapshot(snapshot, fmt.Sprintf("source root %q is not a directory", root))
	}

	excludedPaths := absolutePaths(excludes)
	walkErr := filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		return captureSourceEntry(&snapshot, absRoot, path, entry, walkErr, excludedPaths)
	})
	if walkErr != nil {
		markSnapshotPartial(&snapshot, fmt.Sprintf("walk source root: %v", walkErr))
	}
	sort.Slice(snapshot.Files, func(left, right int) bool {
		return snapshot.Files[left].Path < snapshot.Files[right].Path
	})
	return snapshot
}

// BuildFinalSource compares snapshots and attributes only changes supported by evidence.
func BuildFinalSource(
	starting SourceSnapshot,
	final SourceSnapshot,
	lifecycleChanges []SourceChange,
	modelModifications []FileModification,
) FinalSource {
	evidence := FinalSource{
		Status:   snapshotStatus(starting, final),
		Starting: starting,
		Final:    final,
	}
	if evidence.Status == SourceStatusUnavailable {
		return evidence
	}

	startingFiles := sourceFilesByPath(starting.Files)
	finalFiles := sourceFilesByPath(final.Files)
	paths := make(map[string]struct{}, len(startingFiles)+len(finalFiles))
	for path := range startingFiles {
		paths[path] = struct{}{}
	}
	for path := range finalFiles {
		paths[path] = struct{}{}
	}
	orderedPaths := make([]string, 0, len(paths))
	for path := range paths {
		orderedPaths = append(orderedPaths, path)
	}
	sort.Strings(orderedPaths)

	for _, path := range orderedPaths {
		before := sourceFileContent(startingFiles[path])
		after := sourceFileContent(finalFiles[path])
		if equalSourceContent(before, after) {
			continue
		}
		evidence.Diffs = append(evidence.Diffs, SourceChange{
			Sequence: len(evidence.Diffs) + 1,
			ID:       fmt.Sprintf("final-source-diff-%d", len(evidence.Diffs)+1),
			Path:     path,
			Origin:   sourceChangeOrigin(path, before, after, lifecycleChanges, modelModifications),
			Before:   before,
			After:    after,
			Diff:     unifiedSourceDiff(path, before, after),
			Created:  before == nil,
			Deleted:  after == nil,
		})
	}
	evidence.FilesChangedAtEnd = len(evidence.Diffs)
	return evidence
}

// AppendEvidence adds runner-owned evidence without rewriting adapter event history.
func AppendEvidence(path string, evidence Evidence) error {
	document, fields, err := readArtifactForEvidence(path)
	if err != nil {
		return err
	}
	finalSource := mergeFinalSource(evidence.FinalSource, evidence.LifecycleChanges, document.FileModifications)
	if finalSource.Status == "" {
		delete(fields, "final_source")
	} else {
		fields["final_source"] = mustMarshal(finalSource)
	}
	setEvidenceArray(fields, "lifecycle_changes", evidence.LifecycleChanges)
	setEvidenceArray(fields, "comparison_outcomes", evidence.ComparisonOutcomes)
	setEvidenceArray(fields, "edit_sequences", evidence.EditSequences)
	if err := writeAtomically(path, mustMarshal(fields)); err != nil {
		return fmt.Errorf("write runner audit evidence: %w", err)
	}
	return nil
}

func readArtifactForEvidence(path string) (Artifact, map[string]json.RawMessage, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("read audit artifact for runner evidence: %w", err)
	}
	var document Artifact
	if err := json.Unmarshal(contents, &document); err != nil {
		return Artifact{}, nil, fmt.Errorf("parse audit artifact for runner evidence: %w", err)
	}
	if document.SchemaVersion != SchemaVersion {
		return Artifact{}, nil, fmt.Errorf("audit artifact has schema version %q, want %q", document.SchemaVersion, SchemaVersion)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		return Artifact{}, nil, fmt.Errorf("parse audit artifact fields for runner evidence: %w", err)
	}
	for _, required := range []string{"capture", "run", "events"} {
		if _, ok := fields[required]; !ok {
			return Artifact{}, nil, fmt.Errorf("audit artifact is missing %s", required)
		}
	}
	return document, fields, nil
}

func setEvidenceArray[T any](
	fields map[string]json.RawMessage,
	name string,
	values []T,
) {
	if values == nil {
		delete(fields, name)
		return
	}
	fields[name] = mustMarshal(values)
}

func mergeFinalSource(
	source FinalSource,
	lifecycleChanges []SourceChange,
	modelModifications []FileModification,
) FinalSource {
	if source.Starting.Status == "" || source.Final.Status == "" {
		return source
	}
	if len(source.Diffs) == 0 {
		return BuildFinalSource(source.Starting, source.Final, lifecycleChanges, modelModifications)
	}
	return addModelOrigins(source, lifecycleChanges, modelModifications)
}

func unavailableSnapshot(snapshot SourceSnapshot, message string) SourceSnapshot {
	snapshot.Status = SourceStatusUnavailable
	snapshot.Files = nil
	snapshot.Error = message
	return snapshot
}

func markSnapshotPartial(snapshot *SourceSnapshot, message string) {
	if snapshot.Status == SourceStatusComplete {
		snapshot.Status = SourceStatusPartial
	}
	if snapshot.Error == "" {
		snapshot.Error = message
		return
	}
	snapshot.Error += "; " + message
}

func absolutePaths(paths []string) []string {
	abs := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		resolved, err := filepath.Abs(path)
		if err == nil {
			abs = append(abs, filepath.Clean(resolved))
		}
	}
	return abs
}

func shouldSkipSourcePath(root, path string, excludes []string) bool {
	for _, excluded := range excludes {
		if path == excluded || isWithinPath(excluded, path) {
			return true
		}
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	relative = filepath.ToSlash(relative)
	if relative == "." {
		return false
	}
	return isManagedSourcePath(relative)
}

func captureSourceEntry(
	snapshot *SourceSnapshot,
	root, path string,
	entry fs.DirEntry,
	walkErr error,
	excludes []string,
) error {
	if walkErr != nil {
		markSnapshotPartial(snapshot, fmt.Sprintf("walk %q: %v", path, walkErr))
		return nil
	}
	if shouldSkipSourcePath(root, path, excludes) {
		if entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	if entry.IsDir() {
		return nil
	}
	if !entry.Type().IsRegular() {
		markSnapshotPartial(snapshot, fmt.Sprintf("source path %q is not a regular file", path))
		return nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		markSnapshotPartial(snapshot, fmt.Sprintf("read source file %q: %v", path, err))
		return nil
	}
	if !utf8.Valid(contents) {
		markSnapshotPartial(snapshot, fmt.Sprintf("source file %q is not UTF-8", path))
		return nil
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		markSnapshotPartial(snapshot, fmt.Sprintf("relativize source file %q: %v", path, err))
		return nil
	}
	snapshot.Files = append(snapshot.Files, SourceFile{
		Path:    filepath.ToSlash(relative),
		Content: string(contents),
	})
	return nil
}

func isManagedSourcePath(relative string) bool {
	if relative == ".git" || strings.HasPrefix(relative, ".git/") {
		return true
	}
	if relative == ".local/stbench" || strings.HasPrefix(relative, ".local/stbench/") {
		return true
	}
	if relative == ".local/stcompare" || strings.HasPrefix(relative, ".local/stcompare/") {
		return true
	}
	return relative == "__pycache__" || strings.HasPrefix(relative, "__pycache__/") || strings.Contains(relative, "/__pycache__/")
}

func isWithinPath(parent, path string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func snapshotStatus(starting, final SourceSnapshot) string {
	if !knownSnapshotStatus(starting.Status) || !knownSnapshotStatus(final.Status) {
		return SourceStatusUnavailable
	}
	if starting.Status == SourceStatusUnavailable || final.Status == SourceStatusUnavailable {
		return SourceStatusUnavailable
	}
	if starting.Status == SourceStatusPartial || final.Status == SourceStatusPartial {
		return SourceStatusPartial
	}
	return SourceStatusComplete
}

func knownSnapshotStatus(status string) bool {
	return status == SourceStatusComplete || status == SourceStatusPartial || status == SourceStatusUnavailable
}

func sourceFilesByPath(files []SourceFile) map[string]SourceFile {
	byPath := make(map[string]SourceFile, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}
	return byPath
}

func sourceFileContent(file SourceFile) *string {
	if file.Path == "" {
		return nil
	}
	content := file.Content
	return &content
}

func equalSourceContent(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sourceChangeOrigin(
	path string,
	before, after *string,
	lifecycleChanges []SourceChange,
	modelModifications []FileModification,
) string {
	return sourceChangeEvidence(path, before, after, lifecycleChanges, modelModifications).origin()
}

func addModelOrigins(
	source FinalSource,
	lifecycleChanges []SourceChange,
	modifications []FileModification,
) FinalSource {
	for index := range source.Diffs {
		change := &source.Diffs[index]
		observed := sourceChangeEvidence(change.Path, change.Before, change.After, lifecycleChanges, modifications)
		change.Origin = mergeChangeOrigins(change.Origin, observed)
	}
	return source
}

type changeOriginEvidence struct {
	model     bool
	lifecycle bool
}

func (evidence changeOriginEvidence) origin() string {
	switch {
	case evidence.model && evidence.lifecycle:
		return ChangeOriginModelAndLifecycle
	case evidence.model:
		return ChangeOriginModel
	case evidence.lifecycle:
		return ChangeOriginLifecycle
	default:
		return ChangeOriginUnattributed
	}
}

func mergeChangeOrigins(existing string, observed changeOriginEvidence) string {
	current := originEvidence(existing)
	return changeOriginEvidence{
		model:     current.model || observed.model,
		lifecycle: current.lifecycle || observed.lifecycle,
	}.origin()
}

func originEvidence(origin string) changeOriginEvidence {
	switch origin {
	case ChangeOriginModel:
		return changeOriginEvidence{model: true}
	case ChangeOriginLifecycle:
		return changeOriginEvidence{lifecycle: true}
	case ChangeOriginModelAndLifecycle:
		return changeOriginEvidence{model: true, lifecycle: true}
	default:
		return changeOriginEvidence{}
	}
}

type sourceContentKey struct {
	exists  bool
	content string
}

type sourceTransition struct {
	before sourceContentKey
	after  sourceContentKey
	origin changeOriginEvidence
}

func sourceChangeEvidence(
	path string,
	before, after *string,
	lifecycleChanges []SourceChange,
	modelModifications []FileModification,
) changeOriginEvidence {
	transitions := sourceTransitions(path, lifecycleChanges, modelModifications)
	start := sourceContentKeyOf(before)
	target := sourceContentKeyOf(after)
	if start == target || len(transitions) == 0 {
		return changeOriginEvidence{}
	}
	return shortestSourceChangeEvidence(start, target, transitions)
}

func shortestSourceChangeEvidence(
	start, target sourceContentKey,
	transitions []sourceTransition,
) changeOriginEvidence {
	byBefore := make(map[sourceContentKey][]sourceTransition)
	for _, transition := range transitions {
		byBefore[transition.before] = append(byBefore[transition.before], transition)
	}

	distance := map[sourceContentKey]int{start: 0}
	shortestEvidence := map[sourceContentKey]changeOriginEvidence{start: {}}
	queue := []sourceContentKey{start}
	targetDistance := -1
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if targetDistance >= 0 && distance[current] >= targetDistance {
			continue
		}
		targetDistance = exploreSourceTransitions(
			current,
			target,
			targetDistance,
			byBefore,
			distance,
			shortestEvidence,
			&queue,
		)
	}

	evidence, ok := shortestEvidence[target]
	if !ok {
		return changeOriginEvidence{}
	}
	return evidence
}

func exploreSourceTransitions(
	current, target sourceContentKey,
	targetDistance int,
	byBefore map[sourceContentKey][]sourceTransition,
	distance map[sourceContentKey]int,
	shortestEvidence map[sourceContentKey]changeOriginEvidence,
	queue *[]sourceContentKey,
) int {
	currentDistance := distance[current]
	for _, transition := range byBefore[current] {
		nextDistance := currentDistance + 1
		if targetDistance >= 0 && nextDistance > targetDistance {
			continue
		}
		candidate := combineEvidence(shortestEvidence[current], transition.origin)
		recordShortestEvidence(transition.after, nextDistance, candidate, distance, shortestEvidence, queue)
		if transition.after == target {
			targetDistance = nextDistance
		}
	}
	return targetDistance
}

func recordShortestEvidence(
	content sourceContentKey,
	distanceToContent int,
	candidate changeOriginEvidence,
	distances map[sourceContentKey]int,
	shortestEvidence map[sourceContentKey]changeOriginEvidence,
	queue *[]sourceContentKey,
) {
	knownDistance, seen := distances[content]
	switch {
	case !seen:
		distances[content] = distanceToContent
		shortestEvidence[content] = candidate
		*queue = append(*queue, content)
	case distanceToContent < knownDistance:
		distances[content] = distanceToContent
		shortestEvidence[content] = candidate
		*queue = append(*queue, content)
	case distanceToContent == knownDistance:
		shortestEvidence[content] = intersectEvidence(shortestEvidence[content], candidate)
	}
}

func sourceTransitions(
	path string,
	lifecycleChanges []SourceChange,
	modelModifications []FileModification,
) []sourceTransition {
	transitions := make([]sourceTransition, 0, len(lifecycleChanges)+len(modelModifications))
	indices := make(map[sourceTransitionKey]int, len(lifecycleChanges)+len(modelModifications))
	appendTransition := func(transition sourceTransition) {
		key := sourceTransitionKey{before: transition.before, after: transition.after}
		if index, exists := indices[key]; exists {
			transitions[index].origin = combineEvidence(transitions[index].origin, transition.origin)
			return
		}
		indices[key] = len(transitions)
		transitions = append(transitions, transition)
	}
	for _, change := range lifecycleChanges {
		if change.Path != path || equalSourceContent(change.Before, change.After) {
			continue
		}
		appendTransition(sourceTransition{
			before: sourceContentKeyOf(change.Before),
			after:  sourceContentKeyOf(change.After),
			origin: changeOriginEvidence{lifecycle: true},
		})
	}
	for _, modification := range modelModifications {
		if modification.Path != path {
			continue
		}
		modificationBefore, beforeValid := rawFileContent(modification.Before)
		modificationAfter, afterValid := rawFileContent(modification.After)
		if !beforeValid || !afterValid || equalSourceContent(modificationBefore, modificationAfter) {
			continue
		}
		appendTransition(sourceTransition{
			before: sourceContentKeyOf(modificationBefore),
			after:  sourceContentKeyOf(modificationAfter),
			origin: changeOriginEvidence{model: true},
		})
	}
	return transitions
}

type sourceTransitionKey struct {
	before sourceContentKey
	after  sourceContentKey
}

func combineEvidence(left, right changeOriginEvidence) changeOriginEvidence {
	return changeOriginEvidence{
		model:     left.model || right.model,
		lifecycle: left.lifecycle || right.lifecycle,
	}
}

func intersectEvidence(left, right changeOriginEvidence) changeOriginEvidence {
	return changeOriginEvidence{
		model:     left.model && right.model,
		lifecycle: left.lifecycle && right.lifecycle,
	}
}

func sourceContentKeyOf(content *string) sourceContentKey {
	if content == nil {
		return sourceContentKey{}
	}
	return sourceContentKey{exists: true, content: *content}
}

func rawFileContent(raw json.RawMessage) (*string, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return nil, false
	}
	var content *string
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, false
	}
	return content, true
}

func unifiedSourceDiff(path string, before, after *string) string {
	if equalSourceContent(before, after) {
		return ""
	}
	var output strings.Builder
	if before == nil {
		output.WriteString("--- /dev/null\n")
	} else {
		fmt.Fprintf(&output, "--- a/%s\n", path)
	}
	if after == nil {
		output.WriteString("+++ /dev/null\n")
	} else {
		fmt.Fprintf(&output, "+++ b/%s\n", path)
	}
	beforeLines := sourceLines(before)
	afterLines := sourceLines(after)
	fmt.Fprintf(&output, "@@ -%d,%d +%d,%d @@\n", lineStart(beforeLines), len(beforeLines), lineStart(afterLines), len(afterLines))
	for _, line := range beforeLines {
		output.WriteByte('-')
		output.WriteString(line)
		ensureDiffLineBreak(&output, line)
	}
	for _, line := range afterLines {
		output.WriteByte('+')
		output.WriteString(line)
		ensureDiffLineBreak(&output, line)
	}
	return output.String()
}

func sourceLines(content *string) []string {
	if content == nil || *content == "" {
		return nil
	}
	return strings.SplitAfter(*content, "\n")
}

func lineStart(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	return 1
}

func ensureDiffLineBreak(output *strings.Builder, line string) {
	if !strings.HasSuffix(line, "\n") {
		output.WriteByte('\n')
	}
}
