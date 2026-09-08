// Package audit reads, finalizes, and renders the local-model turn audit.
package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stcompare/benchrecord"
)

// SchemaVersion is the version of the benchmark audit artifact.
const SchemaVersion = "1"

// Artifact is the durable local-model audit document.
type Artifact struct {
	SchemaVersion string                     `json:"schema_version"`
	Run           Run                        `json:"run"`
	Capture       Capture                    `json:"capture"`
	Iterations    []Iteration                `json:"iterations"`
	SharedContent map[string]json.RawMessage `json:"shared_content,omitempty"`
	Events        []Event                    `json:"events"`
}

// Run identifies the benchmark run associated with the audit.
type Run struct {
	ID            string `json:"id"`
	Candidate     string `json:"candidate"`
	Baseline      string `json:"baseline"`
	Agent         string `json:"agent"`
	Model         string `json:"model"`
	Effort        string `json:"effort"`
	Hardware      string `json:"hardware"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at,omitempty"`
	TerminalState string `json:"terminal_state,omitempty"`
}

// Capture describes whether the artifact contains a complete run capture.
type Capture struct {
	Enabled    bool   `json:"enabled"`
	Status     string `json:"status"`
	Complete   bool   `json:"complete"`
	Failure    string `json:"failure,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// Iteration identifies an ordered benchmark iteration and its model turns.
type Iteration struct {
	ID      string   `json:"id"`
	Number  int      `json:"number"`
	TurnIDs []string `json:"turn_ids"`
}

// ContentReference identifies shared message content used by one model input.
type ContentReference struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

// Event is one model-turn lifecycle record in chronological order. Input is
// reconstructable from its shared-content references, and Returned retains its
// exact JSON value from the adapter boundary.
type Event struct {
	Sequence               int                     `json:"sequence"`
	Type                   string                  `json:"type"`
	RunID                  string                  `json:"run_id"`
	IterationID            string                  `json:"iteration_id"`
	Iteration              int                     `json:"iteration"`
	TurnID                 string                  `json:"turn_id"`
	Status                 string                  `json:"status"`
	StartedAt              string                  `json:"started_at"`
	EndedAt                string                  `json:"ended_at,omitempty"`
	DurationMS             int64                   `json:"duration_ms,omitempty"`
	Sampling               map[string]any          `json:"sampling,omitempty"`
	Input                  json.RawMessage         `json:"input"`
	InputContentReferences []ContentReference      `json:"input_content_references,omitempty"`
	Returned               json.RawMessage         `json:"returned,omitempty"`
	ReturnedMessages       []json.RawMessage       `json:"returned_messages,omitempty"`
	Tokens                 *benchrecord.TokenUsage `json:"tokens,omitempty"`
	Error                  string                  `json:"error,omitempty"`
}

// Read loads and validates a versioned audit artifact.
func Read(path string) (Artifact, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("read audit artifact %q: %w", path, err)
	}
	var document Artifact
	if err := json.Unmarshal(contents, &document); err != nil {
		return Artifact{}, fmt.Errorf("parse audit artifact %q: %w", path, err)
	}
	if document.SchemaVersion != SchemaVersion {
		return Artifact{}, fmt.Errorf(
			"audit artifact %q has schema version %q, want %q",
			path,
			document.SchemaVersion,
			SchemaVersion,
		)
	}
	return document, nil
}

// Finalize marks an existing audit with the benchmark's terminal state. The
// update preserves raw event JSON so exact model inputs and responses remain
// reconstructable after finalization.
func Finalize(path string, terminalState string, endedAt time.Time, partial bool, failure string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read audit artifact for finalization: %w", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse audit artifact for finalization: %w", err)
	}

	capture, err := rawObject(document, "capture")
	if err != nil {
		return fmt.Errorf("finalize audit capture: %w", err)
	}
	captureStatus := "complete"
	if partial {
		captureStatus = "partial"
	}
	capture["status"] = rawString(captureStatus)
	capture["complete"] = rawBool(!partial)
	capture["finished_at"] = rawString(endedAt.UTC().Format(time.RFC3339Nano))
	if failure != "" {
		capture["failure"] = rawString(failure)
	}
	document["capture"] = mustMarshal(capture)

	run, err := rawObject(document, "run")
	if err != nil {
		return fmt.Errorf("finalize audit run: %w", err)
	}
	run["ended_at"] = rawString(endedAt.UTC().Format(time.RFC3339Nano))
	run["terminal_state"] = rawString(terminalState)
	document["run"] = mustMarshal(run)

	if err := writeAtomically(path, mustMarshal(document)); err != nil {
		return fmt.Errorf("write finalized audit artifact: %w", err)
	}
	return nil
}

// Build reads an audit artifact and writes its chronological HTML report.
func Build(auditPath, outputPath string) error {
	document, err := Read(auditPath)
	if err != nil {
		return err
	}
	html, err := Render(document)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create audit report directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(html), 0o644); err != nil {
		return fmt.Errorf("write audit report %q: %w", outputPath, err)
	}
	return nil
}

// Render renders the chronological audit without requiring comparison output.
func Render(document Artifact) (string, error) {
	status := captureStatus(document)
	view := pageView{
		SchemaVersion:  document.SchemaVersion,
		Run:            document.Run,
		Status:         status,
		StatusClass:    strings.ReplaceAll(status, " ", "-"),
		Partial:        auditIsPartial(document),
		CaptureFailure: document.Capture.Failure,
		Iterations:     document.Iterations,
		Events:         make([]eventView, 0, len(document.Events)),
	}
	for _, event := range document.Events {
		eventView, err := newEventView(event, document.SharedContent)
		if err != nil {
			return "", err
		}
		view.Events = append(view.Events, eventView)
	}
	return executeTemplate(view)
}

type pageView struct {
	SchemaVersion  string
	Run            Run
	Status         string
	StatusClass    string
	Partial        bool
	CaptureFailure string
	Iterations     []Iteration
	Events         []eventView
}

type eventView struct {
	Sequence         int
	Iteration        int
	IterationID      string
	TurnID           string
	Status           string
	StartedAt        string
	EndedAt          string
	DurationMS       int64
	Sampling         string
	Input            string
	Returned         string
	ReturnedMessages []messageView
	Error            string
	Partial          bool
}

type messageView struct {
	JSON    string
	Content string
	HasText bool
}

func newEventView(event Event, sharedContent map[string]json.RawMessage) (eventView, error) {
	input, err := reconstructInput(event.Input, event.InputContentReferences, sharedContent)
	if err != nil {
		return eventView{}, err
	}
	view := eventView{
		Sequence:    event.Sequence,
		Iteration:   event.Iteration,
		IterationID: event.IterationID,
		TurnID:      event.TurnID,
		Status:      event.Status,
		StartedAt:   event.StartedAt,
		EndedAt:     event.EndedAt,
		DurationMS:  event.DurationMS,
		Sampling:    formatJSON(mustMarshal(event.Sampling)),
		Input:       formatJSON(input),
		Returned:    formatJSON(event.Returned),
		Error:       event.Error,
		Partial:     event.Status != "completed",
	}
	for _, rawMessage := range event.ReturnedMessages {
		message := messageView{JSON: formatJSON(rawMessage)}
		var decoded map[string]any
		if err := json.Unmarshal(rawMessage, &decoded); err == nil {
			if content, ok := decoded["content"].(string); ok && strings.TrimSpace(content) != "" {
				message.Content = content
				message.HasText = true
			}
		}
		view.ReturnedMessages = append(view.ReturnedMessages, message)
	}
	return view, nil
}

func reconstructInput(
	input json.RawMessage,
	references []ContentReference,
	sharedContent map[string]json.RawMessage,
) (json.RawMessage, error) {
	if len(references) == 0 {
		return input, nil
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode model input for content references: %w", err)
	}
	for _, reference := range references {
		rawContent, ok := sharedContent[reference.ID]
		if !ok {
			return nil, fmt.Errorf("shared model content %q is missing", reference.ID)
		}
		var content any
		contentDecoder := json.NewDecoder(bytes.NewReader(rawContent))
		contentDecoder.UseNumber()
		if err := contentDecoder.Decode(&content); err != nil {
			return nil, fmt.Errorf("decode shared model content %q: %w", reference.ID, err)
		}
		if err := setJSONPointer(&document, reference.Path, content); err != nil {
			return nil, err
		}
	}
	reconstructed, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode reconstructed model input: %w", err)
	}
	return reconstructed, nil
}

func setJSONPointer(document *any, pointer string, value any) error {
	if len(pointer) == 0 || pointer[0] != '/' {
		return fmt.Errorf("invalid model input JSON pointer %q", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	for index := range parts {
		parts[index] = strings.ReplaceAll(parts[index], "~1", "/")
		parts[index] = strings.ReplaceAll(parts[index], "~0", "~")
	}
	current := *document
	for _, part := range parts[:len(parts)-1] {
		next, err := jsonPointerChild(current, part)
		if err != nil {
			return fmt.Errorf("resolve model input JSON pointer %q: %w", pointer, err)
		}
		current = next
	}
	if err := replaceJSONPointerChild(current, parts[len(parts)-1], value); err != nil {
		return fmt.Errorf("resolve model input JSON pointer %q: %w", pointer, err)
	}
	return nil
}

func jsonPointerChild(value any, part string) (any, error) {
	object, ok := value.(map[string]any)
	if ok {
		child, exists := object[part]
		if !exists {
			return nil, fmt.Errorf("field %q does not exist", part)
		}
		return child, nil
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("value is not an object or array")
	}
	index, err := parseJSONPointerIndex(part, len(array))
	if err != nil {
		return nil, err
	}
	return array[index], nil
}

func replaceJSONPointerChild(value any, part string, replacement any) error {
	if object, ok := value.(map[string]any); ok {
		if _, exists := object[part]; !exists {
			return fmt.Errorf("field %q does not exist", part)
		}
		object[part] = replacement
		return nil
	}
	array, ok := value.([]any)
	if !ok {
		return fmt.Errorf("value is not an object or array")
	}
	index, err := parseJSONPointerIndex(part, len(array))
	if err != nil {
		return err
	}
	array[index] = replacement
	return nil
}

func parseJSONPointerIndex(value string, length int) (int, error) {
	var index int
	if _, err := fmt.Sscanf(value, "%d", &index); err != nil || index < 0 || index >= length {
		return 0, fmt.Errorf("array index %q is invalid", value)
	}
	return index, nil
}

func captureStatus(document Artifact) string {
	if !document.Capture.Enabled {
		return "not reported"
	}
	if auditIsPartial(document) {
		return "partial"
	}
	return "complete"
}

func auditIsPartial(document Artifact) bool {
	if !document.Capture.Enabled {
		return false
	}
	if document.Capture.Status != "complete" || !document.Capture.Complete {
		return true
	}
	for _, event := range document.Events {
		if event.Status != "completed" {
			return true
		}
	}
	return false
}

func executeTemplate(view pageView) (string, error) {
	var output bytes.Buffer
	if err := auditTemplate.Execute(&output, view); err != nil {
		return "", fmt.Errorf("render audit report: %w", err)
	}
	return output.String(), nil
}

func formatJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "not captured"
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return string(raw)
	}
	return indented.String()
}

func rawObject(document map[string]json.RawMessage, name string) (map[string]json.RawMessage, error) {
	raw, ok := document[name]
	if !ok {
		return nil, fmt.Errorf("missing %s object", name)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func rawString(value string) json.RawMessage {
	return mustMarshal(value)
}

func rawBool(value bool) json.RawMessage {
	return mustMarshal(value)
}

func mustMarshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal audit value: %v", err))
	}
	return encoded
}

func writeAtomically(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".audit-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

var auditTemplate = template.Must(template.New("audit").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Model-turn audit</title>
<style>
body { font: 16px/1.5 system-ui, sans-serif; margin: 2rem auto; max-width: 1100px; padding: 0 1rem; color: #202124; }
header, section, article { margin-bottom: 1.5rem; }
header { border-bottom: 1px solid #d0d7de; padding-bottom: 1rem; }
.status { display: inline-block; border-radius: .25rem; padding: .15rem .45rem; font-weight: 700; text-transform: uppercase; }
.status.partial { background: #fff0c2; color: #7a4b00; }
.status.complete { background: #d8f3dc; color: #1b5e20; }
.status.not-reported { background: #edf0f2; color: #57606a; }
.identity, .turn-meta { display: grid; gap: .4rem 1.5rem; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
.identity span, .turn-meta span { color: #57606a; display: block; font-size: .85rem; }
.iteration { border-top: 3px solid #0969da; padding-top: .5rem; }
.turn { border: 1px solid #d0d7de; border-radius: .4rem; padding: 1rem; }
.turn.partial { border-color: #bf8700; }
pre { background: #f6f8fa; border-radius: .3rem; overflow-x: auto; padding: .75rem; white-space: pre-wrap; word-break: break-word; }
.rationale { border-left: 4px solid #8250df; padding-left: .75rem; }
.label { color: #8250df; font-weight: 700; }
.empty { color: #57606a; }
</style>
</head>
<body>
<header>
<h1>Model-turn audit</h1>
<p>Evidence schema {{.SchemaVersion}} — <span class="status {{.StatusClass}}">{{.Status}}</span></p>
{{with .CaptureFailure}}<p>Audit capture failure: {{.}}</p>{{end}}
<div class="identity">
<div><span>Run</span><strong>{{.Run.ID}}</strong></div>
<div><span>Candidate</span><strong>{{.Run.Candidate}}</strong></div>
<div><span>Baseline</span><strong>{{.Run.Baseline}}</strong></div>
<div><span>Agent / model</span><strong>{{.Run.Agent}} / {{.Run.Model}}</strong></div>
{{with .Run.TerminalState}}<div><span>Terminal state</span><strong>{{.}}</strong></div>{{end}}
<div><span>Started</span><strong>{{.Run.StartedAt}}</strong></div>
{{with .Run.EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
</div>
{{if .Partial}}<p class="empty">This audit is partial. Unfinished activity and incomplete capture are shown as evidence, not as zero activity.</p>{{end}}
</header>
{{if .Events}}
{{range .Events}}
<section class="iteration">
<h2>Iteration {{.Iteration}} <small>({{.IterationID}})</small></h2>
<article class="turn {{if .Partial}}partial{{end}}">
<h3>Model turn {{.TurnID}} — {{.Status}}</h3>
<div class="turn-meta">
<div><span>Chronological sequence</span><strong>{{.Sequence}}</strong></div>
<div><span>Started</span><strong>{{.StartedAt}}</strong></div>
{{with .EndedAt}}<div><span>Ended</span><strong>{{.}}</strong></div>{{end}}
{{if .DurationMS}}<div><span>Inference time</span><strong>{{.DurationMS}} ms</strong></div>{{end}}
</div>
<h4>Exact model input</h4>
<pre>{{.Input}}</pre>
{{if .Sampling}}<h4>Effective sampling settings</h4><pre>{{.Sampling}}</pre>{{end}}
{{if .Returned}}<h4>Returned model response</h4><pre>{{.Returned}}</pre>{{end}}
{{if .ReturnedMessages}}
<h4>Returned model messages</h4>
{{range .ReturnedMessages}}
<pre>{{.JSON}}</pre>
{{if .HasText}}<div class="rationale"><p class="label">Model's stated rationale</p><pre>{{.Content}}</pre></div>{{end}}
{{end}}
{{end}}
{{with .Error}}<p>Turn error: {{.}}</p>{{end}}
{{if .Partial}}<p class="empty">Partial turn: inference or capture did not complete.</p>{{end}}
</article>
</section>
{{end}}
{{else}}
<p class="empty">No model turns were captured.</p>
{{end}}
</body>
</html>
`))
