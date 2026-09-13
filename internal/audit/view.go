package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"stcompare/benchrecord"
)

type iterationView struct {
	ID               string
	Number           int
	Activity         benchrecord.ActivitySummary
	Efficiency       benchrecord.EfficiencySummary
	ActivityReported bool
	Events           []eventView
}

type eventView struct {
	Sequence            int
	Type                string
	Iteration           int
	IterationID         string
	TurnID              string
	Status              string
	StartedAt           string
	EndedAt             string
	DurationMS          int64
	DurationReported    bool
	RecordingOverheadMS int64
	Tokens              *benchrecord.TokenUsage
	TokenStatus         benchrecord.TokenStatus
	Sampling            *payloadView
	Input               *payloadView
	Returned            *payloadView
	ReturnedMessages    []messageView
	Error               string
	Partial             bool
	ModelToolCall       bool
	AdapterOperation    bool
	ID                  string
	ToolCallID          string
	ModelToolCallID     string
	ToolName            string
	Operation           string
	Provenance          string
	Arguments           *payloadView
	Request             *payloadView
	Result              *payloadView
}

type payloadView struct {
	Label string
	JSON  string
}

type messageView struct {
	Payload payloadView
	Content string
	HasText bool
}

type fileHistoryView struct {
	Path          string
	Modifications []modificationView
}

type modificationView struct {
	Sequence           int
	ID                 string
	Path               string
	Operation          string
	ToolName           string
	ModelToolCallID    string
	AdapterOperationID string
	TurnID             string
	IterationID        string
	Iteration          int
	Before             string
	After              string
	BeforeEmpty        bool
	AfterEmpty         bool
	Diff               string
	Created            bool
}

type finalSourceView struct {
	Status            string
	StartingStatus    string
	FinalStatus       string
	FilesChangedAtEnd int
	Diffs             []sourceChangeView
}

type sourceChangeView struct {
	Sequence    int
	ID          string
	Path        string
	Phase       string
	Origin      string
	Before      string
	After       string
	BeforeEmpty bool
	AfterEmpty  bool
	Diff        string
	Created     bool
	Deleted     bool
}

type comparisonOutcomeView struct {
	Sequence    int
	ID          string
	Iteration   int
	IterationID string
	Status      string
	StartedAt   string
	EndedAt     string
	DurationMS  int64
	ExitCode    int
	View        string
	Error       string
}

type editSequenceView struct {
	Sequence               int
	ID                     string
	Iteration              int
	IterationID            string
	ProblemInput           string
	ComparisonBeforeID     string
	EvaluationStatus       string
	SubsequentComparisonID string
	SubsequentComparison   *comparisonOutcomeView
}

func newFinalSourceView(source FinalSource) finalSourceView {
	return finalSourceView{
		Status:            source.Status,
		StartingStatus:    source.Starting.Status,
		FinalStatus:       source.Final.Status,
		FilesChangedAtEnd: source.FilesChangedAtEnd,
		Diffs:             sourceChangeViews(source.Diffs),
	}
}

func sourceChangeViews(changes []SourceChange) []sourceChangeView {
	return mapViews(changes, newSourceChangeView)
}

func newSourceChangeView(change SourceChange) sourceChangeView {
	origin := change.Origin
	if origin == "" {
		origin = ChangeOriginUnattributed
	}
	return sourceChangeView{
		Sequence:    change.Sequence,
		ID:          change.ID,
		Path:        change.Path,
		Phase:       change.Phase,
		Origin:      origin,
		Before:      sourceContent(change.Before),
		After:       sourceContent(change.After),
		BeforeEmpty: isEmptySourceContent(change.Before),
		AfterEmpty:  isEmptySourceContent(change.After),
		Diff:        focusedSourceDiff(change.Path, change.Before, change.After, change.Diff),
		Created:     change.Created,
		Deleted:     change.Deleted,
	}
}

func sourceContent(content *string) string {
	if content == nil {
		return "File did not exist"
	}
	return *content
}

func comparisonOutcomeViews(outcomes []ComparisonOutcome) []comparisonOutcomeView {
	return mapViews(outcomes, newComparisonOutcomeView)
}

func newComparisonOutcomeView(outcome ComparisonOutcome) comparisonOutcomeView {
	return comparisonOutcomeView{
		Sequence:    outcome.Sequence,
		ID:          outcome.ID,
		Iteration:   outcome.Iteration,
		IterationID: outcome.IterationID,
		Status:      outcome.Status,
		StartedAt:   outcome.StartedAt,
		EndedAt:     outcome.EndedAt,
		DurationMS:  outcome.DurationMS,
		ExitCode:    outcome.ExitCode,
		View:        formatJSON(outcome.View),
		Error:       outcome.Error,
	}
}

func editSequenceViews(sequences []EditSequence, outcomes []ComparisonOutcome) []editSequenceView {
	byID := make(map[string]comparisonOutcomeView, len(outcomes))
	for _, outcome := range mapViews(outcomes, newComparisonOutcomeView) {
		byID[outcome.ID] = outcome
	}
	return mapViews(sequences, func(sequence EditSequence) editSequenceView {
		view := editSequenceView{
			Sequence:               sequence.Sequence,
			ID:                     sequence.ID,
			Iteration:              sequence.Iteration,
			IterationID:            sequence.IterationID,
			ProblemInput:           sequence.ProblemInput,
			ComparisonBeforeID:     sequence.ComparisonBeforeID,
			EvaluationStatus:       sequence.EvaluationStatus,
			SubsequentComparisonID: sequence.SubsequentComparisonID,
		}
		if outcome, ok := byID[sequence.SubsequentComparisonID]; ok {
			view.SubsequentComparison = &outcome
		}
		return view
	})
}

func mapViews[Source any, View any](values []Source, newView func(Source) View) []View {
	views := make([]View, 0, len(values))
	for _, value := range values {
		views = append(views, newView(value))
	}
	return views
}

func fileHistories(modifications []FileModification) []fileHistoryView {
	ordered := append([]FileModification(nil), modifications...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].Sequence < ordered[right].Sequence
	})
	histories := make([]fileHistoryView, 0)
	indices := make(map[string]int)
	for _, modification := range ordered {
		index, exists := indices[modification.Path]
		if !exists {
			index = len(histories)
			indices[modification.Path] = index
			histories = append(histories, fileHistoryView{Path: modification.Path})
		}
		histories[index].Modifications = append(
			histories[index].Modifications,
			newModificationView(modification),
		)
	}
	return histories
}

func newModificationView(modification FileModification) modificationView {
	before, _ := rawFileContent(modification.Before)
	after, _ := rawFileContent(modification.After)
	return modificationView{
		Sequence:           modification.Sequence,
		ID:                 modification.ID,
		Path:               modification.Path,
		Operation:          modification.Operation,
		ToolName:           modification.ToolName,
		ModelToolCallID:    modification.ModelToolCallID,
		AdapterOperationID: modification.AdapterOperationID,
		TurnID:             modification.TurnID,
		IterationID:        modification.IterationID,
		Iteration:          modification.Iteration,
		Before:             formatFileContent(modification.Before),
		After:              formatFileContent(modification.After),
		BeforeEmpty:        isEmptySourceContent(before),
		AfterEmpty:         isEmptySourceContent(after),
		Diff:               focusedSourceDiff(modification.Path, before, after, modification.Diff),
		Created:            modification.Created,
	}
}

func focusedSourceDiff(path string, before, after *string, captured string) string {
	focused := unifiedSourceDiff(path, before, after)
	if focused == "" {
		// Keep the captured diff when source pointers cannot establish a transition.
		return captured
	}
	return focused
}

func isEmptySourceContent(content *string) bool {
	return content != nil && *content == ""
}

func formatFileContent(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "File did not exist"
	}
	var content string
	if err := json.Unmarshal(raw, &content); err == nil {
		return content
	}
	return formatJSON(raw)
}

func newEventView(event Event, sharedContent map[string]json.RawMessage) (eventView, error) {
	input, err := reconstructInput(event.Input, event.InputContentReferences, sharedContent)
	if err != nil {
		return eventView{}, err
	}
	argumentsLabel := "Tool arguments (JSON)"
	resultLabel := "Tool result (JSON)"
	if event.Type == "adapter_operation" {
		argumentsLabel = "Adapter Operation arguments (JSON)"
		resultLabel = "Adapter Operation result (JSON)"
	}
	view := eventView{
		Sequence:            event.Sequence,
		Type:                event.Type,
		Iteration:           event.Iteration,
		IterationID:         event.IterationID,
		TurnID:              event.TurnID,
		Status:              event.Status,
		StartedAt:           event.StartedAt,
		EndedAt:             event.EndedAt,
		DurationMS:          event.DurationMS,
		DurationReported:    event.EndedAt != "" || event.Status == "completed" || event.Status == "failed",
		RecordingOverheadMS: event.RecordingOverheadMS,
		Tokens:              event.Tokens,
		TokenStatus:         tokenStatus(event.Tokens),
		Sampling:            newSamplingPayload(event.Sampling),
		Input:               newRequiredJSONPayload("Exact model input (JSON)", input),
		Returned:            newJSONPayload("Returned model response (JSON)", event.Returned),
		Error:               event.Error,
		Partial:             EventIsIncomplete(event),
		ModelToolCall:       event.Type == "model_tool_call",
		AdapterOperation:    event.Type == "adapter_operation",
		ID:                  event.ID,
		ToolCallID:          event.ToolCallID,
		ModelToolCallID:     event.ModelToolCallID,
		ToolName:            event.ToolName,
		Operation:           event.Operation,
		Provenance:          event.Provenance,
		Arguments:           newJSONPayload(argumentsLabel, event.Arguments),
		Request:             newJSONPayload("Tool request (JSON)", event.Request),
		Result:              newJSONPayload(resultLabel, event.Result),
	}
	for index, rawMessage := range event.ReturnedMessages {
		messagePayload := newRequiredJSONPayload(
			fmt.Sprintf("Returned model message %d (JSON)", index+1),
			rawMessage,
		)
		message := messageView{Payload: *messagePayload}
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

func newSamplingPayload(settings map[string]any) *payloadView {
	if settings == nil {
		return nil
	}
	return &payloadView{
		Label: "Effective sampling settings (JSON)",
		JSON:  formatJSON(mustMarshal(settings)),
	}
}

func newJSONPayload(label string, raw json.RawMessage) *payloadView {
	if !hasJSONPayload(raw) {
		return nil
	}
	return &payloadView{Label: label, JSON: formatJSON(raw)}
}

func newRequiredJSONPayload(label string, raw json.RawMessage) *payloadView {
	return &payloadView{Label: label, JSON: formatJSON(raw)}
}

func hasJSONPayload(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	hasContent := len(trimmed) > 0
	isNull := bytes.Equal(trimmed, []byte("null"))
	return hasContent && !isNull
}

func tokenStatus(tokens *benchrecord.TokenUsage) benchrecord.TokenStatus {
	if tokens == nil {
		return benchrecord.TokenStatusUnknown
	}
	return benchrecord.TokenStatusComplete
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

func formatJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "not captured"
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return string(raw)
	}
	return indented.String()
}
