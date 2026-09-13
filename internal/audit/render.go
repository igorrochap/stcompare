// Package audit reads, finalizes, and renders the local-model turn audit.
package audit

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"stcompare/benchrecord"
)

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
	efficiency := SummarizeEfficiency(document)
	view := pageView{
		SchemaVersion:       document.SchemaVersion,
		Run:                 document.Run,
		Status:              status,
		StatusClass:         strings.ReplaceAll(status, " ", "-"),
		Partial:             auditIsPartial(document),
		CaptureFailure:      document.Capture.Failure,
		Activity:            SummarizeActivity(document),
		ActivityReported:    activityEvidenceReported(document),
		Efficiency:          efficiency,
		EfficiencyReported:  efficiency.Status != benchrecord.EfficiencyStatusNotReported,
		CaptureEnabled:      document.Capture.Enabled,
		Iterations:          make([]iterationView, 0, len(document.Iterations)),
		FileHistories:       fileHistories(document.FileModifications),
		FinalSourceReported: document.FinalSource.Status != "",
		FinalSource:         newFinalSourceView(document.FinalSource),
		LifecycleChanges:    sourceChangeViews(document.LifecycleChanges),
		ComparisonOutcomes:  comparisonOutcomeViews(document.ComparisonOutcomes),
		EditSequences:       editSequenceViews(document.EditSequences, document.ComparisonOutcomes),
	}
	for _, iteration := range document.Iterations {
		view.Iterations = append(view.Iterations, iterationView{
			ID:               iteration.ID,
			Number:           iteration.Number,
			Activity:         summarizeIterationActivity(document, iteration.ID),
			Efficiency:       summarizeIterationEfficiency(document, iteration.ID),
			ActivityReported: activityEvidenceReported(document),
			Events:           []eventView{},
		})
	}
	for _, event := range document.Events {
		eventView, err := newEventView(event, document.SharedContent)
		if err != nil {
			return "", err
		}
		iterationIndex := view.ensureIteration(event.IterationID, event.Iteration)
		view.Iterations[iterationIndex].Events = append(view.Iterations[iterationIndex].Events, eventView)
	}
	for index := range view.Iterations {
		view.Iterations[index].Activity = summarizeIterationActivity(document, view.Iterations[index].ID)
		view.Iterations[index].Efficiency = summarizeIterationEfficiency(document, view.Iterations[index].ID)
	}
	return executeTemplate(view)
}

func (view *pageView) ensureIteration(iterationID string, number int) int {
	for index, iteration := range view.Iterations {
		if iteration.ID == iterationID {
			return index
		}
	}
	view.Iterations = append(view.Iterations, iterationView{
		ID:               iterationID,
		Number:           number,
		ActivityReported: view.ActivityReported,
		Events:           []eventView{},
	})
	return len(view.Iterations) - 1
}

type pageView struct {
	SchemaVersion       string
	Run                 Run
	Status              string
	StatusClass         string
	Partial             bool
	CaptureFailure      string
	Activity            benchrecord.ActivitySummary
	ActivityReported    bool
	Efficiency          benchrecord.EfficiencySummary
	EfficiencyReported  bool
	CaptureEnabled      bool
	Iterations          []iterationView
	FileHistories       []fileHistoryView
	FinalSourceReported bool
	FinalSource         finalSourceView
	LifecycleChanges    []sourceChangeView
	ComparisonOutcomes  []comparisonOutcomeView
	EditSequences       []editSequenceView
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
		if EventIsIncomplete(event) {
			return true
		}
	}
	if document.FinalSource.Status == SourceStatusPartial || document.FinalSource.Status == SourceStatusUnavailable {
		return true
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

//go:embed audit.gohtml
var auditTemplateText string

var auditTemplate = template.Must(template.New("audit").Parse(auditTemplateText))
