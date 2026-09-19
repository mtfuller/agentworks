// Package inspector is AgentWorks' MCP inspector: a full-screen Bubble Tea
// UI that connects to an mcp artifact's server -- starting its declared
// "command" as a process, or reaching its remote http/sse endpoint (via
// internal/mcpclient) -- and lets a user browse the tools it exposes, fill
// in and submit a call against one, and inspect the result; and, for a
// server that offers them, browse its resources and prompts too --
// `agentworks run`'s entry point (see cmd/run.go).
//
// It's a separate package from internal/tui rather than folded into that
// project browser: this is a different screen with a different
// lifecycle (connect once, then browse/call/inspect until the user
// quits) and its own async connect/list/call flow, not another pane in
// the kind -> artifact -> detail drill-down. It reuses that package's
// established idioms where they fit -- bubbles list/viewport, a
// huh.Form embedded as a child model for the call-parameter form (see
// schemaform.go), a plain helpStyle/statusStyle footer -- rather than
// inventing new ones.
package inspector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/mcpclient"
	"github.com/mtfuller/agentworks/internal/version"
)

// pane identifies which of the inspector's screens is active, once
// connected (see Model.connectDone/connectErr for the pre-connection
// state, which isn't its own pane -- there's nothing to switch between
// yet).
type pane int

const (
	paneList    pane = iota // tool list (left) + detail/result viewport (right)
	paneForm                // a huh.Form for the selected tool's call parameters
	paneHistory             // full-screen list of this session's past calls
	paneLogs                // full-screen raw JSON-RPC/stderr transcript
)

var (
	headerStyle  = lipgloss.NewStyle().Bold(true)
	helpStyle    = lipgloss.NewStyle().Faint(true)
	statusStyle  = lipgloss.NewStyle().Bold(true)
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	listPaneStyle   = lipgloss.NewStyle().PaddingRight(1)
	detailPaneStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(lipgloss.Color("240")).
			PaddingLeft(1)
)

// detailPaneOverhead is how many columns detailPaneStyle's border+padding
// consume beyond the content itself -- subtracted when sizing the detail
// viewport so its wrapped content doesn't overflow the terminal width.
const detailPaneOverhead = 2

// toolItem adapts an mcpclient.Tool for bubbles/list.
type toolItem struct {
	tool mcpclient.Tool
}

func (i toolItem) Title() string { return i.tool.Name }
func (i toolItem) Description() string {
	if i.tool.Description == "" {
		return "(no description)"
	}
	return i.tool.Description
}
func (i toolItem) FilterValue() string { return i.tool.Name }

// Model is the inspector's root Bubble Tea model.
type Model struct {
	artifactName string
	target       mcpclient.Target
	missingAuth  []string

	conn *mcpclient.Conn

	// connectDone is set once the initial connect+initialize+tools/list
	// attempt finishes, successfully or not -- connectErr distinguishes
	// the two. Before that, View() shows a standalone connecting screen
	// rather than the normal pane layout, since there's nothing to browse
	// yet.
	connectDone bool
	connectErr  error
	serverInfo  mcpclient.ClientInfo

	spinner     spinner.Model
	calling     bool
	callingTool string

	pane pane
	// focusRight, in paneList, sends navigation keys to the detail
	// viewport (to scroll a long result) instead of the tool list; tab
	// toggles it.
	focusRight bool

	// section is which list paneList is showing; resources and prompts exist
	// only for a server that advertised them.
	section      section
	hasResources bool
	hasPrompts   bool
	toolList     list.Model
	resourceList list.Model
	promptList   list.Model
	detail       viewport.Model

	historyList list.Model
	logView     viewport.Model
	logLines    []string

	activeForm *callForm
	formTool   mcpclient.Tool
	// formPrompt is set instead of formTool's call when the active form is
	// collecting a prompt's arguments.
	formPrompt *mcpclient.Prompt

	history     []callRecord
	shownResult *callRecord

	traceCh chan string

	statusMsg     string
	width, height int
}

// New builds an inspector Model for artifact a, ready to be run via
// tea.NewProgram (see Run in app.go). target says how to reach the server:
// a local command to start (the shape mcpconfig.ServerFor builds for
// export, except here it's actually executed) or a remote URL -- see
// cmd/run.go. missingAuth is purely informational, shown as a header
// warning.
func New(a *artifact.Artifact, target mcpclient.Target, missingAuth []string) Model {
	toolList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	toolList.Title = "Tools"

	resourceList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	resourceList.Title = "Resources"

	promptList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	promptList.Title = "Prompts"

	historyList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	historyList.Title = "Call history"

	return Model{
		artifactName: a.Name,
		target:       target,
		missingAuth:  missingAuth,
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot)),
		toolList:     toolList,
		resourceList: resourceList,
		promptList:   promptList,
		detail:       viewport.New(0, 0),
		historyList:  historyList,
		logView:      viewport.New(0, 0),
		traceCh:      make(chan string, 256),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, connectCmd(m.target, m.traceCh), waitForTrace(m.traceCh))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applySizes()
		return m, nil

	case spinner.TickMsg:
		if !m.connectDone || m.calling {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case traceLineMsg:
		m.appendLogLine(string(msg))
		return m, waitForTrace(m.traceCh)

	case connectedMsg:
		return m.handleConnected(msg)

	case callResultMsg:
		return m.handleCallResult(msg)

	case resourceResultMsg:
		return m.handleResourceResult(msg)

	case promptResultMsg:
		return m.handlePromptResult(msg)
	}

	if !m.connectDone {
		if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "q" || key.String() == "esc") {
			return m, tea.Quit
		}
		return m, nil
	}

	if m.pane == paneForm {
		return m.updateForm(msg)
	}

	if key, ok := msg.(tea.KeyMsg); ok && !m.isFiltering() {
		if handled, next, cmd := m.handleKey(key); handled {
			return next, cmd
		}
	}

	var cmd tea.Cmd
	switch m.pane {
	case paneList:
		if m.focusRight {
			m.detail, cmd = m.detail.Update(msg)
		} else {
			l := m.currentList()
			*l, cmd = l.Update(msg)
			m.syncDetailToSelection()
		}
	case paneHistory:
		m.historyList, cmd = m.historyList.Update(msg)
	case paneLogs:
		m.logView, cmd = m.logView.Update(msg)
	}
	return m, cmd
}

// handleKey handles single-key shortcuts that aren't just "forward to the
// focused widget" -- pane switches, opening the call form, etc. Returns
// handled=false to fall through to the generic per-pane dispatch in
// Update.
func (m Model) handleKey(key tea.KeyMsg) (handled bool, next Model, cmd tea.Cmd) {
	switch m.pane {
	case paneList:
		switch key.String() {
		case "q", "esc":
			return true, m, tea.Quit
		case "enter":
			if !m.focusRight {
				updated, c := m.activateSelection()
				return true, updated, c
			}
		case "1", "2", "3":
			if next, ok := m.switchSection(key.String()); ok {
				return true, next, nil
			}
		case "tab":
			m.focusRight = !m.focusRight
			return true, m, nil
		case "h":
			m.pane = paneHistory
			return true, m, nil
		case "l":
			m.pane = paneLogs
			m.logView.GotoBottom()
			return true, m, nil
		}
	case paneHistory:
		switch key.String() {
		case "q", "esc":
			m.pane = paneList
			return true, m, nil
		case "enter":
			updated, c := m.showSelectedHistory()
			return true, updated, c
		}
	case paneLogs:
		switch key.String() {
		case "q", "esc":
			m.pane = paneList
			return true, m, nil
		}
	}
	return false, m, nil
}

func (m Model) isFiltering() bool {
	switch m.pane {
	case paneList:
		return m.currentList().FilterState() == list.Filtering
	case paneHistory:
		return m.historyList.FilterState() == list.Filtering
	default:
		return false
	}
}

// applySizes lays out the tool list / detail split (paneList), and sizes
// the full-screen history/log panes and any active form, from the
// terminal size in m.width/m.height. Called on every WindowSizeMsg and
// once connect succeeds (so a form or list opened before the first
// resize event still has real dimensions).
func (m *Model) applySizes() {
	if m.width == 0 || m.height == 0 {
		return
	}

	bodyHeight := m.height - 3 // header line + footer line + a blank line between them and the body
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	listWidth := m.width / 3
	if listWidth < 24 {
		listWidth = 24
	}
	if listWidth > m.width-20 {
		listWidth = m.width - 20
	}
	if listWidth < 1 {
		listWidth = m.width
	}
	detailWidth := m.width - listWidth - detailPaneOverhead
	if detailWidth < 1 {
		detailWidth = 1
	}

	m.toolList.SetSize(listWidth, bodyHeight)
	m.resourceList.SetSize(listWidth, bodyHeight)
	m.promptList.SetSize(listWidth, bodyHeight)
	m.detail.Width = detailWidth
	m.detail.Height = bodyHeight

	m.historyList.SetSize(m.width, bodyHeight)
	m.logView.Width = m.width
	m.logView.Height = bodyHeight
	m.logView.SetContent(m.renderLog())

	if m.activeForm != nil {
		m.activeForm.Form = m.activeForm.Form.WithWidth(m.width).WithHeight(bodyHeight)
	}
}

// -- connect --

// connectedMsg reports the outcome of the initial connect+initialize+
// tools/list sequence (see connectCmd). Exactly one of err or (conn,
// tools) is meaningful. resources and prompts are listed only if the server
// advertised them, and failing to list them isn't fatal: it is reported in
// warnings and the connection still works for tools.
type connectedMsg struct {
	conn      *mcpclient.Conn
	info      mcpclient.InitializeResult
	tools     []mcpclient.Tool
	resources []mcpclient.Resource
	prompts   []mcpclient.Prompt
	warnings  []string
	err       error
}

// connectCmd connects to the artifact's server, performs the initialize
// handshake, and lists its tools (and resources and prompts, if it offers
// them) -- everything `agentworks run` needs before there's anything to show
// the user. traceCh receives every raw JSON-RPC message and stderr line as
// they happen, for the log pane -- wired up before Initialize so the
// handshake itself is visible there too.
func connectCmd(target mcpclient.Target, traceCh chan<- string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		conn, err := mcpclient.Open(ctx, target,
			func(direction string, raw []byte) { sendNonBlocking(traceCh, formatTraceLine(direction, raw)) },
			func(line string) { sendNonBlocking(traceCh, formatStderrLine(line)) },
		)
		if err != nil {
			return connectedMsg{err: fmt.Errorf("connecting to %s: %w", describeTarget(target), err)}
		}

		info, err := conn.Initialize(ctx, "agentworks", version.GetShortVersion())
		if err != nil {
			_ = conn.Close()
			return connectedMsg{err: fmt.Errorf("initialize: %w", err)}
		}
		tools, err := conn.ListTools(ctx)
		if err != nil {
			_ = conn.Close()
			return connectedMsg{err: fmt.Errorf("tools/list: %w", err)}
		}

		msg := connectedMsg{conn: conn, info: info, tools: tools}
		if info.HasCapability("resources") {
			if msg.resources, err = conn.ListResources(ctx); err != nil {
				msg.warnings = append(msg.warnings, "resources/list: "+err.Error())
			}
		}
		if info.HasCapability("prompts") {
			if msg.prompts, err = conn.ListPrompts(ctx); err != nil {
				msg.warnings = append(msg.warnings, "prompts/list: "+err.Error())
			}
		}
		return msg
	}
}

func (m Model) handleConnected(msg connectedMsg) (tea.Model, tea.Cmd) {
	m.connectDone = true
	if msg.err != nil {
		m.connectErr = msg.err
		return m, nil
	}

	m.conn = msg.conn
	m.serverInfo = msg.info.ServerInfo

	items := make([]list.Item, len(msg.tools))
	for i, t := range msg.tools {
		items[i] = toolItem{tool: t}
	}
	m.toolList.SetItems(items)

	// A section appears only for a server that advertised it (and listed at
	// least one entry): a tools-only server looks exactly as it always has.
	m.hasResources = len(msg.resources) > 0
	m.hasPrompts = len(msg.prompts) > 0
	resItems := make([]list.Item, len(msg.resources))
	for i, r := range msg.resources {
		resItems[i] = resourceItem{res: r}
	}
	m.resourceList.SetItems(resItems)
	promptItems := make([]list.Item, len(msg.prompts))
	for i, p := range msg.prompts {
		promptItems[i] = promptItem{prompt: p}
	}
	m.promptList.SetItems(promptItems)
	if len(msg.warnings) > 0 {
		m.statusMsg = strings.Join(msg.warnings, "; ")
	}
	m.applySizes()
	m.syncDetailToSelection()
	return m, nil
}

// -- trace/log --

// traceLineMsg is one already-formatted line for the log pane, delivered
// via traceCh (see waitForTrace) since Client.OnTrace/OnStderr fire from
// goroutines outside the Bubble Tea event loop.
type traceLineMsg string

// waitForTrace blocks for the next line on ch and returns it as a
// tea.Msg -- the standard Bubble Tea "listen on a channel" pattern.
// Model.Update re-issues this after every traceLineMsg to keep listening.
func waitForTrace(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return traceLineMsg(line)
	}
}

// sendNonBlocking drops a trace line rather than blocking the MCP
// client's own read-loop/write goroutines if the log pane's channel is
// ever full (a UI consumer being slow must never stall the protocol
// client) -- the log pane is a best-effort debugging aid, not a
// guaranteed-complete record.
func sendNonBlocking(ch chan<- string, line string) {
	select {
	case ch <- line:
	default:
	}
}

func (m *Model) appendLogLine(line string) {
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
	}
	m.logView.SetContent(m.renderLog())
	m.logView.GotoBottom()
}

func (m Model) renderLog() string {
	if len(m.logLines) == 0 {
		return helpStyle.Render("(no traffic yet)")
	}
	return strings.Join(m.logLines, "\n")
}

// -- call form --

// openCallForm builds and switches to a huh.Form for the currently
// selected tool's parameters (see schemaform.go's newCallForm).
func (m Model) openCallForm() (Model, tea.Cmd) {
	item, ok := m.toolList.SelectedItem().(toolItem)
	if !ok {
		return m, nil
	}
	cf, err := newCallForm(item.tool)
	if err != nil {
		m.statusMsg = fmt.Sprintf("%s: %v", item.tool.Name, err)
		return m, nil
	}
	if m.width > 0 {
		cf.Form = cf.Form.WithWidth(m.width).WithHeight(m.height - 3)
	}

	m.activeForm = cf
	m.formTool = item.tool
	m.pane = paneForm
	m.statusMsg = ""
	return m, m.activeForm.Form.Init()
}

// updateForm drives the active call form. ctrl+x is this inspector's own
// explicit cancel key rather than relying on huh's default Quit binding
// (ctrl+c) -- that's already intercepted at the top of Update to quit the
// whole program, so the form would otherwise have no reachable way to
// abort back to the tool list.
func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+x" {
		m.pane = paneList
		m.activeForm = nil
		m.formPrompt = nil
		m.statusMsg = ""
		return m, nil
	}

	updated, cmd := m.activeForm.Form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		m.activeForm.Form = f
	}

	switch m.activeForm.Form.State {
	case huh.StateCompleted:
		return m.submitForm()
	case huh.StateAborted:
		m.pane = paneList
		m.activeForm = nil
		m.formPrompt = nil
		return m, nil
	default:
		return m, cmd
	}
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	args, err := m.activeForm.Arguments()
	tool := m.formTool
	m.pane = paneList
	m.activeForm = nil

	if err != nil {
		m.statusMsg = fmt.Sprintf("%s: %v", tool.Name, err)
		return m, nil
	}
	if m.conn == nil {
		m.statusMsg = "not connected"
		return m, nil
	}

	if prompt := m.formPrompt; prompt != nil {
		m.formPrompt = nil
		m.calling = true
		m.callingTool = prompt.Name
		m.statusMsg = fmt.Sprintf("Rendering %s...", prompt.Name)
		return m, tea.Batch(getPromptCmd(m.conn, *prompt, stringArgs(args)), m.spinner.Tick)
	}

	m.calling = true
	m.callingTool = tool.Name
	m.statusMsg = fmt.Sprintf("Calling %s...", tool.Name)
	return m, tea.Batch(callToolCmd(m.conn, tool.Name, args), m.spinner.Tick)
}

// -- calling a tool --

type callResultMsg struct{ record callRecord }

func callToolCmd(conn *mcpclient.Conn, toolName string, args map[string]any) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err := conn.CallTool(ctx, toolName, args)
		return callResultMsg{record: callRecord{
			at:       start,
			toolName: toolName,
			args:     args,
			result:   result,
			err:      err,
			duration: time.Since(start),
		}}
	}
}

func (m Model) handleCallResult(msg callResultMsg) (tea.Model, tea.Cmd) {
	m.calling = false
	m.callingTool = ""
	m.history = append(m.history, msg.record)
	m.rebuildHistoryList()

	rec := msg.record
	m.shownResult = &rec
	m.selectToolByName(rec.toolName)
	m.detail.SetContent(m.renderDetail())
	m.detail.GotoTop()

	if rec.ok() {
		m.statusMsg = fmt.Sprintf("Called %s in %s", rec.toolName, rec.duration.Round(time.Millisecond))
	} else if rec.err != nil {
		m.statusMsg = fmt.Sprintf("%s: %v", rec.toolName, rec.err)
	} else {
		m.statusMsg = fmt.Sprintf("%s reported an error -- see the result pane", rec.toolName)
	}
	return m, nil
}

func (m *Model) rebuildHistoryList() {
	items := make([]list.Item, len(m.history))
	for i, r := range m.history {
		items[i] = historyItem{record: r}
	}
	m.historyList.SetItems(items)
}

// -- selection/detail sync --

func (m *Model) syncDetailToSelection() {
	switch m.section {
	case sectionResources:
		m.shownResult = nil
		if it, ok := m.resourceList.SelectedItem().(resourceItem); ok {
			m.detail.SetContent(renderResourceDetail(it.res))
		} else {
			m.detail.SetContent("")
		}
		m.detail.GotoTop()
		return
	case sectionPrompts:
		m.shownResult = nil
		if it, ok := m.promptList.SelectedItem().(promptItem); ok {
			m.detail.SetContent(renderPromptDetail(it.prompt))
		} else {
			m.detail.SetContent("")
		}
		m.detail.GotoTop()
		return
	}
	item, ok := m.toolList.SelectedItem().(toolItem)
	if !ok {
		m.shownResult = nil
		m.detail.SetContent("")
		return
	}
	m.shownResult = m.lastResultFor(item.tool.Name)
	m.detail.SetContent(m.renderDetail())
	m.detail.GotoTop()
}

func (m Model) lastResultFor(name string) *callRecord {
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].toolName == name {
			rec := m.history[i]
			return &rec
		}
	}
	return nil
}

func (m *Model) selectToolByName(name string) {
	for i, it := range m.toolList.Items() {
		if ti, ok := it.(toolItem); ok && ti.tool.Name == name {
			m.toolList.Select(i)
			return
		}
	}
}

func (m Model) showSelectedHistory() (Model, tea.Cmd) {
	item, ok := m.historyList.SelectedItem().(historyItem)
	if !ok {
		return m, nil
	}
	rec := item.record
	m.shownResult = &rec
	m.selectToolByName(rec.toolName)
	m.detail.SetContent(m.renderDetail())
	m.detail.GotoTop()
	m.pane = paneList
	return m, nil
}

func (m Model) renderDetail() string {
	item, ok := m.toolList.SelectedItem().(toolItem)
	if !ok {
		return helpStyle.Render("Select a tool from the list.")
	}

	var b strings.Builder
	b.WriteString(headerStyle.Render(item.tool.Name))
	b.WriteString("\n")
	if item.tool.Description != "" {
		b.WriteString(item.tool.Description)
		b.WriteString("\n")
	}

	schema, err := parseSchema(item.tool.InputSchema)
	if err != nil {
		fmt.Fprintf(&b, "\n%s\n", errorStyle.Render("invalid inputSchema: "+err.Error()))
	} else {
		fields := paramFields(schema)
		if len(fields) == 0 {
			b.WriteString("\nParameters: none\n")
		} else {
			b.WriteString("\nParameters:\n")
			for _, f := range fields {
				req := ""
				if f.Required {
					req = " (required)"
				}
				fType := f.Type
				if fType == "" {
					fType = "any"
				}
				fmt.Fprintf(&b, "  %s: %s%s", f.Name, fType, req)
				if f.Description != "" {
					fmt.Fprintf(&b, " -- %s", f.Description)
				}
				b.WriteString("\n")
			}
		}
	}

	if m.shownResult != nil {
		b.WriteString("\n")
		b.WriteString(strings.Repeat("-", 40))
		b.WriteString("\n\n")
		b.WriteString(renderCallResult(*m.shownResult))
	}

	return b.String()
}

// -- view --

func (m Model) View() string {
	if !m.connectDone {
		return m.connectingView()
	}
	// A failed connection still shows the error screen by default, but
	// "l" (handled the same as any other pane -- see handleKey) switches
	// to the log pane so the real diagnostic (e.g. the server process's
	// stderr traceback) is reachable instead of just the protocol-level
	// "connection closed" error errorView() shows on its own.
	if m.connectErr != nil && m.pane != paneLogs {
		return m.errorView()
	}

	var body string
	switch m.pane {
	case paneForm:
		body = m.activeForm.Form.View()
	case paneHistory:
		body = m.historyList.View()
	case paneLogs:
		body = m.logView.View()
	default:
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			listPaneStyle.Render(m.currentList().View()),
			detailPaneStyle.Render(m.detail.View()),
		)
	}

	out := m.renderHeader() + "\n" + body + "\n" + helpStyle.Render(m.helpText())
	if m.statusMsg != "" {
		out += "\n" + statusStyle.Render(m.statusMsg)
	}
	return out
}

func (m Model) connectingView() string {
	verb := "starting"
	if m.target.IsRemote() {
		verb = "connecting to"
	}
	return fmt.Sprintf("\n  %s %s %s...\n\n  %s\n\n  %s",
		m.spinner.View(), verb, m.artifactName, helpStyle.Render(describeTarget(m.target)), helpStyle.Render("q: quit"))
}

func (m Model) errorView() string {
	return fmt.Sprintf("\n  %s\n\n  %v\n\n  %s",
		errorStyle.Render("Connection failed"), m.connectErr,
		helpStyle.Render("l: view raw output (stderr, JSON-RPC traffic) for the real cause  •  q: quit"))
}

func (m Model) renderHeader() string {
	title := headerStyle.Render(fmt.Sprintf("agentworks run: %s", m.artifactName))

	var status string
	switch {
	case m.connectErr != nil:
		status = errorStyle.Render("connection failed")
	case m.calling:
		status = m.spinner.View() + fmt.Sprintf(" calling %s...", m.callingTool)
	default:
		s := "connected"
		if m.serverInfo.Name != "" {
			s = fmt.Sprintf("connected to %s %s", m.serverInfo.Name, m.serverInfo.Version)
		}
		status = successStyle.Render(s)
	}

	line := title + "   " + status
	if tabs := m.renderTabs(); tabs != "" {
		line += "   " + tabs
	}
	if len(m.missingAuth) > 0 {
		line += "   " + warnStyle.Render("missing env: "+strings.Join(m.missingAuth, ", "))
	}
	return line
}

func (m Model) helpText() string {
	switch m.pane {
	case paneList:
		focus := "list"
		if m.focusRight {
			focus = "result"
		}
		action := "call tool"
		switch m.section {
		case sectionResources:
			action = "read resource"
		case sectionPrompts:
			action = "render prompt"
		}
		switching := ""
		if m.hasResources || m.hasPrompts {
			switching = "1/2/3: section  •  "
		}
		return fmt.Sprintf("enter: %s  •  %stab: focus %s  •  h: history  •  l: logs  •  /: filter  •  q: quit", action, switching, focus)
	case paneForm:
		return "enter: next field  •  ctrl+x: cancel"
	case paneHistory:
		return "enter: view result  •  /: filter  •  esc: back"
	case paneLogs:
		return "↑/↓  pgup/pgdn: scroll  •  esc: back"
	default:
		return "ctrl+c: quit"
	}
}

// -- sections --

// currentList is the list paneList is showing for the active section.
func (m *Model) currentList() *list.Model {
	switch m.section {
	case sectionResources:
		return &m.resourceList
	case sectionPrompts:
		return &m.promptList
	default:
		return &m.toolList
	}
}

// switchSection handles 1/2/3, moving to the tools, resources, or prompts
// list if the server offers it. ok is false for a section it doesn't.
func (m Model) switchSection(key string) (Model, bool) {
	var target section
	switch key {
	case "1":
		target = sectionTools
	case "2":
		if !m.hasResources {
			return m, false
		}
		target = sectionResources
	case "3":
		if !m.hasPrompts {
			return m, false
		}
		target = sectionPrompts
	default:
		return m, false
	}
	m.section = target
	m.focusRight = false
	m.statusMsg = ""
	m.syncDetailToSelection()
	return m, true
}

// renderTabs shows the sections a server offers, highlighting the active one.
// It is empty for a tools-only server, which has nothing to switch between.
func (m Model) renderTabs() string {
	if !m.hasResources && !m.hasPrompts {
		return ""
	}
	labels := []struct {
		s      section
		shown  bool
		number string
	}{
		{sectionTools, true, "1"},
		{sectionResources, m.hasResources, "2"},
		{sectionPrompts, m.hasPrompts, "3"},
	}
	var parts []string
	for _, l := range labels {
		if !l.shown {
			continue
		}
		text := l.number + " " + l.s.label()
		if l.s == m.section {
			text = headerStyle.Render("[" + text + "]")
		} else {
			text = helpStyle.Render(text)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

// activateSelection is enter on the browse list: call a tool, read a
// resource, or render a prompt.
func (m Model) activateSelection() (Model, tea.Cmd) {
	switch m.section {
	case sectionResources:
		it, ok := m.resourceList.SelectedItem().(resourceItem)
		if !ok || m.conn == nil {
			return m, nil
		}
		m.calling = true
		m.callingTool = it.res.Name
		m.statusMsg = "Reading " + it.res.URI + "..."
		return m, tea.Batch(readResourceCmd(m.conn, it.res), m.spinner.Tick)

	case sectionPrompts:
		it, ok := m.promptList.SelectedItem().(promptItem)
		if !ok || m.conn == nil {
			return m, nil
		}
		if len(it.prompt.Arguments) == 0 {
			m.calling = true
			m.callingTool = it.prompt.Name
			m.statusMsg = "Rendering " + it.prompt.Name + "..."
			return m, tea.Batch(getPromptCmd(m.conn, it.prompt, nil), m.spinner.Tick)
		}
		cf, err := newCallForm(promptAsTool(it.prompt))
		if err != nil {
			m.statusMsg = fmt.Sprintf("%s: %v", it.prompt.Name, err)
			return m, nil
		}
		if m.width > 0 {
			cf.Form = cf.Form.WithWidth(m.width).WithHeight(m.height - 3)
		}
		prompt := it.prompt
		m.activeForm = cf
		m.formPrompt = &prompt
		m.formTool = promptAsTool(prompt)
		m.pane = paneForm
		m.statusMsg = ""
		return m, m.activeForm.Form.Init()
	}
	return m.openCallForm()
}

func (m Model) handleResourceResult(msg resourceResultMsg) (tea.Model, tea.Cmd) {
	m.calling = false
	m.callingTool = ""
	m.detail.SetContent(renderResourceResult(msg))
	m.detail.GotoTop()
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("%s: %v", msg.res.URI, msg.err)
	} else {
		m.statusMsg = fmt.Sprintf("Read %s in %s", msg.res.URI, msg.took.Round(time.Millisecond))
	}
	return m, nil
}

func (m Model) handlePromptResult(msg promptResultMsg) (tea.Model, tea.Cmd) {
	m.calling = false
	m.callingTool = ""
	m.detail.SetContent(renderPromptResult(msg))
	m.detail.GotoTop()
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("%s: %v", msg.prompt.Name, msg.err)
	} else {
		m.statusMsg = fmt.Sprintf("Rendered %s in %s", msg.prompt.Name, msg.took.Round(time.Millisecond))
	}
	return m, nil
}
