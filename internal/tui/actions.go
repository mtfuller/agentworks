package tui

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

// formPurpose records what an active paneForm is for, so finishForm knows
// which commit* method to run once it completes.
type formPurpose int

const (
	formNone formPurpose = iota
	formCreate
	formExport
)

// startCreateForm opens the create-artifact wizard (the same one
// `agentworks new` uses -- see wizard.go), pre-filling Kind from whatever's
// currently in view.
func (m Model) startCreateForm() (tea.Model, tea.Cmd) {
	var defaults NewArtifactAnswers
	switch m.pane {
	case paneArtifacts:
		defaults.Kind = string(m.currentKind)
	case paneKinds:
		if item, ok := m.kindList.SelectedItem().(kindItem); ok {
			defaults.Kind = string(item.kind)
		}
	}

	form, answers := newArtifactForm(defaults)
	m.newAnswers = answers
	m.formPurpose = formCreate
	m.formReturnPane = m.pane
	m.activeForm = m.sizeForm(form)
	m.statusMsg = ""
	m.pane = paneForm
	return m, m.activeForm.Init()
}

// startExportForm opens the export wizard for whichever artifact is
// currently selected/viewed. If no registered target supports its kind,
// it reports that in the footer instead of opening an empty form.
func (m Model) startExportForm() (tea.Model, tea.Cmd) {
	var subject *artifact.Artifact
	switch m.pane {
	case paneArtifacts:
		if item, ok := m.artifactList.SelectedItem().(artifactItem); ok {
			subject = item.a
		}
	case paneDetail:
		subject = m.currentArtifact
	}
	if subject == nil {
		return m, nil
	}

	form, answers := exportForm(subject)
	if form == nil {
		m.statusMsg = fmt.Sprintf("no registered target supports %s artifacts yet", subject.Kind)
		return m, nil
	}

	m.exportSubject = subject
	m.exportAnswers = answers
	m.formPurpose = formExport
	m.formReturnPane = m.pane
	m.activeForm = m.sizeForm(form)
	m.statusMsg = ""
	m.pane = paneForm
	return m, m.activeForm.Init()
}

// sizeForm applies the browser's known terminal size to a freshly built
// form, so it renders consistently with the rest of the UI instead of
// falling back to huh's own default sizing.
func (m Model) sizeForm(form *huh.Form) *huh.Form {
	if m.width > 0 && m.height > 2 {
		form = form.WithWidth(m.width).WithHeight(m.height - 2)
	}
	return form
}

// updateForm drives the active huh.Form and hands control back once it's
// done (completed or cancelled).
func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = wsm.Width, wsm.Height
	}

	updated, cmd := m.activeForm.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		m.activeForm = f
	}

	switch m.activeForm.State {
	case huh.StateCompleted:
		return m.finishForm(true)
	case huh.StateAborted:
		return m.finishForm(false)
	default:
		return m, cmd
	}
}

// finishForm hands control back to the pane the form was opened from, and
// on successful completion (not a cancel), commits the action.
func (m Model) finishForm(completed bool) (tea.Model, tea.Cmd) {
	purpose := m.formPurpose
	m.activeForm = nil
	m.formPurpose = formNone
	m.pane = m.formReturnPane

	if !completed {
		m.newAnswers = nil
		m.exportAnswers = nil
		m.exportSubject = nil
		return m, nil
	}

	switch purpose {
	case formCreate:
		return m.commitCreate()
	case formExport:
		return m.commitExport()
	}
	return m, nil
}

// commitCreate mirrors cmd/new.go's own flow exactly: resolve the kind,
// fall back to the project's default targets when none were chosen, then
// scaffold.New -- the CLI and TUI never disagree about what "new" does.
func (m Model) commitCreate() (tea.Model, tea.Cmd) {
	answers := m.newAnswers
	m.newAnswers = nil

	kind, err := artifact.ParseKind(answers.Kind)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}

	targetList := answers.Targets
	if len(targetList) == 0 {
		if pm, err := project.Load(m.root); err == nil {
			targetList = pm.Targets
		}
	}

	a, err := scaffold.New(m.root, kind, answers.Name, scaffold.Options{
		Description: answers.Description,
		Targets:     targetList,
	})
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}

	m.statusMsg = fmt.Sprintf("Created %s %q", kind, a.Name)
	return m.refreshAfterCreate(kind)
}

// refreshAfterCreate rebuilds the kind list's counts and drills straight
// into the newly-created artifact's kind, so the browser shows it
// immediately instead of a stale list.
func (m Model) refreshAfterCreate(kind artifact.Kind) (tea.Model, tea.Cmd) {
	items := make([]list.Item, 0, len(artifact.Kinds()))
	for _, k := range artifact.Kinds() {
		found, errs := project.Discover(m.root, k)
		if len(errs) > 0 {
			m.err = errs[0]
			continue
		}
		items = append(items, kindItem{kind: k, count: len(found)})
	}
	m.kindList.SetItems(items)

	found, errs := project.Discover(m.root, kind)
	if len(errs) > 0 {
		m.err = errs[0]
		return m, nil
	}
	artifactItems := make([]list.Item, len(found))
	for i, a := range found {
		artifactItems[i] = artifactItem{a: a}
	}
	m.artifactList = list.New(artifactItems, list.NewDefaultDelegate(), m.width, m.height-2)
	m.artifactList.Title = kind.DirName()
	m.currentKind = kind
	m.pane = paneArtifacts
	return m, nil
}

// commitExport mirrors cmd/export.go's own flow exactly (same Exporter
// interface call, same default output directory).
func (m Model) commitExport() (tea.Model, tea.Cmd) {
	subject := m.exportSubject
	answers := m.exportAnswers
	m.exportSubject = nil
	m.exportAnswers = nil

	exporter, err := targets.GetExporter(answers.Target)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}

	// Rooted at the project directory (not the process's cwd, which could
	// be anywhere the browser happened to be launched from) -- matches
	// cmd/export.go's own "dist" default, just anchored consistently with
	// how the rest of the model already addresses everything via m.root.
	outDir := filepath.Join(m.root, "dist")
	dest, err := exporter.Export(subject, outDir, targets.ExportOptions{Zip: answers.Zip})
	if err != nil {
		m.statusMsg = fmt.Sprintf("export failed: %v", err)
		return m, nil
	}

	m.statusMsg = fmt.Sprintf("Exported %s to %s for %s", subject.Name, dest, answers.Target)
	return m, nil
}
