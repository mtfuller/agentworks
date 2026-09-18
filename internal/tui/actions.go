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
// `agentworks new` uses -- see wizard.go), pre-filling Kind from the
// active tab and Targets from the project's own default (agentworks.yaml
// "targets:") so accepting the form as-is just works, instead of forcing
// every artifact to re-pick the same targets from a blank multi-select.
func (m Model) startCreateForm() (tea.Model, tea.Cmd) {
	defaults := NewArtifactAnswers{
		Kind:    string(m.currentKind()),
		Targets: append([]string(nil), m.defaultTargets...),
	}

	form, answers := newArtifactForm(defaults)
	m.newAnswers = answers
	m.formPurpose = formCreate
	m.formReturnPane = m.pane
	m.activeForm = m.sizeForm(form)
	m.clearStatus()
	m.pane = paneForm
	return m, m.activeForm.Init()
}

// selectedArtifact is whichever artifact is currently selected (in
// paneBrowse) or being viewed (in paneDetail) -- what "e"/"t" act on.
func (m Model) selectedArtifact() *artifact.Artifact {
	switch m.pane {
	case paneBrowse:
		if item, ok := m.artifactLists[m.currentKind()].SelectedItem().(artifactItem); ok {
			return item.a
		}
	case paneDetail:
		return m.currentArtifact
	}
	return nil
}

// startExportForm opens the export wizard for whichever artifact is
// currently selected/viewed. If no registered target supports its kind,
// it reports that in the footer instead of opening an empty form.
func (m Model) startExportForm() (tea.Model, tea.Cmd) {
	subject := m.selectedArtifact()
	if subject == nil {
		return m, nil
	}

	form, answers := exportForm(subject)
	if form == nil {
		m.setStatus(statusWarn, "no registered target supports %s artifacts yet", subject.Kind)
		return m, nil
	}

	m.exportSubject = subject
	m.exportAnswers = answers
	m.formPurpose = formExport
	m.formReturnPane = m.pane
	m.activeForm = m.sizeForm(form)
	m.clearStatus()
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
// then scaffold.New -- the CLI and TUI never disagree about what "new"
// does. Unlike before, there's no post-hoc "fall back to project defaults
// if Targets is empty" step here: startCreateForm already pre-filled
// Targets from the project's defaults before the form ever opened, so an
// empty answers.Targets now means the user deliberately cleared it, not
// that nothing was chosen.
func (m Model) commitCreate() (tea.Model, tea.Cmd) {
	answers := m.newAnswers
	m.newAnswers = nil

	kind, err := artifact.ParseKind(answers.Kind)
	if err != nil {
		m.setStatus(statusError, "%v", err)
		return m, nil
	}

	a, err := scaffold.New(m.root, kind, answers.Name, scaffold.Options{
		Description: answers.Description,
		Targets:     answers.Targets,
		Template:    answers.Template,
	})
	if err != nil {
		m.setStatus(statusError, "%v", err)
		return m, nil
	}

	m.setStatus(statusSuccess, "Created %s %q", kind, a.Name)
	return m.refreshAfterCreate(kind)
}

// refreshAfterCreate rebuilds the given kind's artifact list (and its tab
// label's count) and switches the browse pane to that kind's tab, so the
// browser shows the newly-created (or imported) artifact immediately
// instead of a stale list.
func (m Model) refreshAfterCreate(kind artifact.Kind) (tea.Model, tea.Cmd) {
	found, errs := project.Discover(m.root, kind)
	if len(errs) > 0 {
		m.err = errs[0]
		return m, nil
	}
	items := make([]list.Item, len(found))
	for i, a := range found {
		items[i] = artifactItem{a: a}
	}

	al := m.artifactLists[kind]
	al.SetItems(items)
	m.artifactLists[kind] = al

	idx := kindIndex(kind)
	m.browseTabs.labels[idx] = fmt.Sprintf("%s (%d)", kind.DirName(), len(found))
	m.browseTabs.active = idx
	m.pane = paneBrowse
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
		m.setStatus(statusError, "%v", err)
		return m, nil
	}

	// Rooted at the project directory (not the process's cwd, which could
	// be anywhere the browser happened to be launched from) -- matches
	// cmd/export.go's own "dist" default, just anchored consistently with
	// how the rest of the model already addresses everything via m.root.
	outDir := filepath.Join(m.root, "dist")
	dest, err := exporter.Export(subject, outDir, targets.ExportOptions{Zip: answers.Zip})
	if err != nil {
		m.setStatus(statusError, "export failed: %v", err)
		return m, nil
	}

	m.setStatus(statusSuccess, "Exported %s to %s for %s", subject.Name, dest, answers.Target)
	return m, nil
}
