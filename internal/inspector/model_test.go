package inspector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/mcpclient"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

// The inspector's model is driven here the way Bubble Tea drives it -- by
// feeding it messages and running the commands it returns -- against a fake
// MCP server over HTTP, so no terminal or real server process is needed.

type fakeOpts struct {
	resources bool
	prompts   bool
}

// fakeMCP serves a streamable-HTTP MCP server with one tool ("echo"), and
// optionally a resource ("mem://notes") and two prompts (one taking an argument).
func fakeMCP(t *testing.T, opts fakeOpts) *httptest.Server {
	t.Helper()
	reply := func(w http.ResponseWriter, id *int64, result any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	fail := func(w http.ResponseWriter, id *int64, msg string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": msg}})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "initialize":
			caps := map[string]any{"tools": map[string]any{}}
			if opts.resources {
				caps["resources"] = map[string]any{}
			}
			if opts.prompts {
				caps["prompts"] = map[string]any{}
			}
			reply(w, req.ID, map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "fake", "version": "1.0"}, "capabilities": caps})
		case "tools/list":
			reply(w, req.ID, map[string]any{"tools": []map[string]any{{
				"name": "echo", "description": "Echoes its input",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
			}}})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			reply(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "echo: " + toString(p.Arguments["text"])}}})
		case "resources/list":
			reply(w, req.ID, map[string]any{"resources": []map[string]any{{"uri": "mem://notes", "name": "notes", "description": "Some notes", "mimeType": "text/plain"}}})
		case "resources/read":
			reply(w, req.ID, map[string]any{"contents": []map[string]any{{"uri": "mem://notes", "text": "remember the milk"}}})
		case "prompts/list":
			reply(w, req.ID, map[string]any{"prompts": []map[string]any{
				{"name": "hello", "description": "Says hello"},
				{"name": "greet", "description": "Greets someone", "arguments": []map[string]any{{"name": "who", "description": "Whom to greet", "required": true}}},
			}})
		case "prompts/get":
			var p struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			if p.Name == "greet" && p.Arguments["who"] == "" {
				fail(w, req.ID, "who is required")
				return
			}
			text := "Hello!"
			if p.Arguments["who"] != "" {
				text = "Hello, " + p.Arguments["who"] + "!"
			}
			reply(w, req.ID, map[string]any{"description": "A greeting", "messages": []map[string]any{{"role": "user", "content": map[string]any{"type": "text", "text": text}}}})
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func testArtifact(name string) *artifact.Artifact {
	return &artifact.Artifact{Frontmatter: artifact.Frontmatter{Kind: artifact.KindMCP, Name: name}}
}

// connected builds a Model and completes its connect step against target,
// returning the model ready to browse (and a cleanup that closes the connection).
func connected(t *testing.T, target mcpclient.Target) Model {
	t.Helper()
	m := New(testArtifact("demo"), target, nil)
	msg := connectCmd(target, m.traceCh)()
	next, _ := m.Update(msg)
	m = next.(Model)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	t.Cleanup(func() {
		if m.conn != nil {
			m.conn.Close()
		}
	})
	return m
}

func httpTarget(srv *httptest.Server) mcpclient.Target {
	return mcpclient.Target{Transport: mcpclient.TransportHTTP, URL: srv.URL}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+x":
		return tea.KeyMsg{Type: tea.KeyCtrlX}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m Model, k string) (Model, tea.Cmd) {
	next, cmd := m.Update(key(k))
	return next.(Model), cmd
}

// run executes a command the way the Bubble Tea runtime would and returns the
// messages it produced, unwrapping a Batch. Anything that takes more than a few
// seconds (a spinner tick that never came due) is abandoned.
func run(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	var mu sync.Mutex
	var msgs []tea.Msg
	var wg sync.WaitGroup
	var exec func(c tea.Cmd)
	exec = func(c tea.Cmd) {
		defer wg.Done()
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		select {
		case msg := <-done:
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					if sub != nil {
						wg.Add(1)
						go exec(sub)
					}
				}
				return
			}
			if msg != nil {
				mu.Lock()
				msgs = append(msgs, msg)
				mu.Unlock()
			}
		case <-time.After(3 * time.Second):
		}
	}
	wg.Add(1)
	go exec(cmd)
	wg.Wait()
	return msgs
}

func feed(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range run(t, cmd) {
		if _, isTick := msg.(interface{ String() string }); isTick && false {
			continue
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestToolsOnlyServerLooksLikeItAlwaysDid(t *testing.T) {
	m := connected(t, httpTarget(fakeMCP(t, fakeOpts{})))

	if m.hasResources || m.hasPrompts {
		t.Fatal("a tools-only server must not grow resources or prompts sections")
	}
	view := m.View()
	if !strings.Contains(view, "echo") || !strings.Contains(view, "connected to fake 1.0") {
		t.Errorf("view should list the tool and the server, got:\n%s", view)
	}
	if strings.Contains(view, "1 Tools") {
		t.Errorf("a tools-only server has nothing to switch between, so no tabs: %s", view)
	}
	before := m.section
	m, _ = press(m, "2")
	m, _ = press(m, "3")
	if m.section != before {
		t.Error("2 and 3 must do nothing without those sections")
	}
}

func TestResourcesAndPromptsAppearWhenOffered(t *testing.T) {
	m := connected(t, httpTarget(fakeMCP(t, fakeOpts{resources: true, prompts: true})))
	if !m.hasResources || !m.hasPrompts {
		t.Fatalf("hasResources=%v hasPrompts=%v, want both", m.hasResources, m.hasPrompts)
	}
	for _, want := range []string{"1 Tools", "2 Resources", "3 Prompts"} {
		if !strings.Contains(m.View(), want) {
			t.Errorf("header should show the %q tab:\n%s", want, m.View())
		}
	}
	if !strings.Contains(m.helpText(), "1/2/3: section") {
		t.Errorf("help should mention switching sections: %s", m.helpText())
	}

	// Resources: browse, then read.
	m, _ = press(m, "2")
	if m.section != sectionResources || !strings.Contains(m.View(), "notes") {
		t.Fatalf("section = %v; view:\n%s", m.section, m.View())
	}
	if !strings.Contains(m.detail.View(), "mem://notes") || !strings.Contains(m.detail.View(), "text/plain") {
		t.Errorf("detail should describe the resource before it is read:\n%s", m.detail.View())
	}
	if !strings.Contains(m.helpText(), "read resource") {
		t.Errorf("help text = %q", m.helpText())
	}
	m, cmd := press(m, "enter")
	if !m.calling || !strings.Contains(m.statusMsg, "Reading mem://notes") {
		t.Errorf("enter should start reading: calling=%v status=%q", m.calling, m.statusMsg)
	}
	m = feed(t, m, cmd)
	if m.calling {
		t.Error("the read result should end the busy state")
	}
	if !strings.Contains(m.detail.View(), "remember the milk") || !strings.Contains(m.statusMsg, "Read mem://notes") {
		t.Errorf("the resource's contents should be shown:\n%s\nstatus: %s", m.detail.View(), m.statusMsg)
	}

	// Prompts: one with no arguments renders straight away.
	m, _ = press(m, "3")
	if m.section != sectionPrompts {
		t.Fatalf("section = %v, want prompts", m.section)
	}
	if !strings.Contains(m.helpText(), "render prompt") {
		t.Errorf("help text = %q", m.helpText())
	}
	m, cmd = press(m, "enter") // "hello" sorts/lists first
	m = feed(t, m, cmd)
	if !strings.Contains(m.detail.View(), "Hello!") || !strings.Contains(m.detail.View(), "user") {
		t.Errorf("the rendered prompt should show its messages:\n%s", m.detail.View())
	}

	// A prompt with a required argument opens a form; cancelling returns to the list.
	next, _ := m.Update(key("j")) // move to "greet"
	m = next.(Model)
	m, _ = press(m, "enter")
	if m.pane != paneForm || m.formPrompt == nil || m.formPrompt.Name != "greet" {
		t.Fatalf("a prompt with arguments should open a form: pane=%v formPrompt=%+v", m.pane, m.formPrompt)
	}
	m, _ = press(m, "ctrl+x")
	if m.pane != paneList || m.formPrompt != nil || m.activeForm != nil {
		t.Errorf("ctrl+x should cancel the prompt form: pane=%v", m.pane)
	}

	// Back to tools.
	m, _ = press(m, "1")
	if m.section != sectionTools {
		t.Error("1 should return to the tool list")
	}
}

func TestPromptFailureIsShownNotFatal(t *testing.T) {
	m := connected(t, httpTarget(fakeMCP(t, fakeOpts{prompts: true})))
	m, _ = press(m, "3")
	next, _ := m.Update(key("j")) // "greet"
	m = next.(Model)
	msg := getPromptCmd(m.conn, m.promptList.SelectedItem().(promptItem).prompt, nil)() // no required argument
	next, _ = m.Update(msg)
	m = next.(Model)
	if !strings.Contains(m.detail.View(), "who is required") || !strings.Contains(m.statusMsg, "who is required") {
		t.Errorf("a server error should be reported:\n%s\nstatus: %s", m.detail.View(), m.statusMsg)
	}
}

func TestCallingAToolRecordsHistoryAndLogs(t *testing.T) {
	m := connected(t, httpTarget(fakeMCP(t, fakeOpts{})))

	msg := callToolCmd(m.conn, "echo", map[string]any{"text": "hi"})()
	next, _ := m.Update(msg)
	m = next.(Model)
	if len(m.history) != 1 || !m.history[0].ok() {
		t.Fatalf("history = %+v, want one successful call", m.history)
	}
	if !strings.Contains(m.detail.View(), "echo: hi") || !strings.Contains(m.statusMsg, "Called echo") {
		t.Errorf("the result should be shown:\n%s\nstatus: %s", m.detail.View(), m.statusMsg)
	}

	m, _ = press(m, "h")
	if m.pane != paneHistory || !strings.Contains(m.View(), "echo") {
		t.Errorf("h should open the history pane: %v\n%s", m.pane, m.View())
	}
	m, _ = press(m, "enter") // view that result
	if m.pane != paneList {
		t.Error("enter on a history entry should return to the list showing its result")
	}

	m, _ = press(m, "l")
	if m.pane != paneLogs {
		t.Fatal("l should open the raw-traffic log")
	}
	// The trace channel was fed by the connect/call; drain it into the log pane.
	for {
		select {
		case line := <-m.traceCh:
			next, _ := m.Update(traceLineMsg(line))
			m = next.(Model)
			continue
		default:
		}
		break
	}
	// The viewport follows the newest traffic, so check the recorded lines
	// rather than what happens to be scrolled into view.
	all := strings.Join(m.logLines, "\n")
	if !strings.Contains(all, "initialize") || !strings.Contains(all, "tools/call") {
		t.Errorf("the log should record the handshake and the call:\n%s", all)
	}
	if strings.Contains(all, "Authorization") {
		t.Error("the log must never show headers")
	}
	m, _ = press(m, "esc")
	if m.pane != paneList {
		t.Error("esc should leave the log")
	}
}

func TestToolFormOpensAndCancels(t *testing.T) {
	m := connected(t, httpTarget(fakeMCP(t, fakeOpts{})))
	m, _ = press(m, "enter")
	if m.pane != paneForm || m.activeForm == nil {
		t.Fatalf("enter on a tool should open its parameter form, pane=%v", m.pane)
	}
	if m.formPrompt != nil {
		t.Error("a tool's form is not a prompt's")
	}
	m, _ = press(m, "ctrl+x")
	if m.pane != paneList || m.activeForm != nil {
		t.Error("ctrl+x should cancel the form")
	}
}

func TestFocusAndQuit(t *testing.T) {
	m := connected(t, httpTarget(fakeMCP(t, fakeOpts{})))
	m, _ = press(m, "tab")
	if !m.focusRight || !strings.Contains(m.helpText(), "focus result") {
		t.Errorf("tab should focus the result pane: %v / %q", m.focusRight, m.helpText())
	}
	m, _ = press(m, "tab")
	if m.focusRight {
		t.Error("a second tab should return focus to the list")
	}
	if _, cmd := press(m, "q"); cmd == nil {
		t.Error("q should quit")
	} else if msg := cmd(); msg != tea.Quit() {
		t.Errorf("q returned %v, want tea.Quit", msg)
	}
	if _, cmd := m.Update(key("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should quit")
	}
}

func TestConnectionFailureShowsTheErrorAndKeepsLogsReachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/mcp?token=secret-in-url"
	srv.Close() // nothing listening

	target := mcpclient.Target{Transport: mcpclient.TransportHTTP, URL: url}
	m := New(testArtifact("demo"), target, []string{"API_TOKEN"})

	if !strings.Contains(m.View(), "connecting to") {
		t.Errorf("a remote target should say it is connecting: %s", m.View())
	}
	msg := connectCmd(target, m.traceCh)()
	next, _ := m.Update(msg)
	m = next.(Model)

	view := m.View()
	if !strings.Contains(view, "Connection failed") {
		t.Fatalf("the failure should be shown:\n%s", view)
	}
	if strings.Contains(view, "secret-in-url") {
		t.Errorf("a token in the URL leaked into the error screen:\n%s", view)
	}
	m, _ = press(m, "l")
	if m.pane != paneLogs || strings.Contains(m.View(), "Connection failed") {
		t.Error("l should reach the log pane even from the failure screen")
	}
	m, _ = press(m, "q") // back from the logs to the failure screen
	if m.pane != paneList || !strings.Contains(m.View(), "Connection failed") {
		t.Error("q in the log pane should return to the failure screen")
	}
	if _, cmd := press(m, "q"); cmd == nil {
		t.Error("q on the failure screen should quit")
	}
}

func TestBeforeConnectingOnlyQuitWorks(t *testing.T) {
	m := New(testArtifact("demo"), mcpclient.Target{Command: "python3 x.py"}, nil)
	if !strings.Contains(m.View(), "starting") || !strings.Contains(m.View(), "python3 x.py") {
		t.Errorf("a local target should say it is starting the command:\n%s", m.View())
	}
	if _, cmd := press(m, "j"); cmd != nil {
		t.Error("navigation keys do nothing before connecting")
	}
	if _, cmd := press(m, "q"); cmd == nil {
		t.Error("q should quit even before connecting")
	}
}

func TestHeaderWarnsAboutMissingEnvironment(t *testing.T) {
	srv := fakeMCP(t, fakeOpts{})
	m := New(testArtifact("demo"), httpTarget(srv), []string{"API_TOKEN", "OTHER"})
	next, _ := m.Update(connectCmd(httpTarget(srv), m.traceCh)())
	m = next.(Model)
	t.Cleanup(func() { m.conn.Close() })
	if !strings.Contains(m.View(), "missing env: API_TOKEN, OTHER") {
		t.Errorf("the header should warn about unset variables:\n%s", m.View())
	}
}

func TestResourceListFailureIsAWarningNotAFailure(t *testing.T) {
	// A server that advertises resources but errors on resources/list still
	// connects, with the tools working and the problem in the status line.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "f"},
				"capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
			}})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"tools": []map[string]any{{"name": "t"}}}})
		case "resources/list":
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -1, "message": "boom"}})
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	m := connected(t, httpTarget(srv))
	if m.connectErr != nil {
		t.Fatalf("connectErr = %v, want the connection to succeed", m.connectErr)
	}
	if !strings.Contains(m.statusMsg, "resources/list") || !strings.Contains(m.statusMsg, "boom") {
		t.Errorf("status = %q, want the resource listing failure", m.statusMsg)
	}
	if m.hasResources {
		t.Error("no resources were listed, so no section")
	}
}

// One end-to-end pass through a real local server: the scaffolded Python MCP
// server, started as a process, called through the same code the UI uses.
func TestLocalStdioServerEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	a, err := scaffold.New(t.TempDir(), artifact.KindMCP, "local", scaffold.Options{Description: "A local server used to check the inspector end to end."})
	if err != nil {
		t.Fatal(err)
	}
	target := mcpclient.Target{Transport: mcpclient.TransportStdio, Command: "python3 src/server.py", Dir: a.Dir}
	m := connected(t, target)
	if m.connectErr != nil {
		t.Fatalf("connectErr = %v", m.connectErr)
	}
	if !strings.Contains(m.View(), "hello") {
		t.Errorf("the scaffold's tool should be listed:\n%s", m.View())
	}
	msg := callToolCmd(m.conn, "hello", map[string]any{"name": "world"})()
	next, _ := m.Update(msg)
	m = next.(Model)
	if !strings.Contains(m.detail.View(), "Hello, world!") {
		t.Errorf("the call's result should be shown:\n%s", m.detail.View())
	}
}

// ---- small helpers --------------------------------------------------------

func TestDescribeTargetHidesCredentials(t *testing.T) {
	tests := []struct {
		target mcpclient.Target
		want   string
	}{
		{mcpclient.Target{Command: "node server.js"}, "node server.js"},
		{mcpclient.Target{Transport: "http", URL: "https://user:pw@example.com/mcp?token=abc"}, "https://example.com/mcp"},
		{mcpclient.Target{Transport: "sse", URL: "://bad"}, "(remote server)"},
	}
	for _, tt := range tests {
		if got := describeTarget(tt.target); got != tt.want {
			t.Errorf("describeTarget(%+v) = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestPromptAsToolBuildsAFormSchema(t *testing.T) {
	tool := promptAsTool(mcpclient.Prompt{
		Name: "greet", Description: "Greets",
		Arguments: []mcpclient.PromptArgument{{Name: "who", Description: "Whom", Required: true}, {Name: "tone"}},
	})
	if tool.Name != "greet" || tool.Description != "Greets" {
		t.Errorf("tool = %+v", tool)
	}
	schema, err := parseSchema(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	fields := paramFields(schema)
	if len(fields) != 2 || fields[0].Name != "tone" || fields[1].Name != "who" || !fields[1].Required || fields[0].Required {
		t.Errorf("fields = %+v, want tone (optional) and who (required), both strings", fields)
	}
	if _, err := newCallForm(tool); err != nil {
		t.Errorf("newCallForm() error = %v, want a prompt's arguments to make a form", err)
	}
}

func TestStringArgsDropsEmptyValues(t *testing.T) {
	got := stringArgs(map[string]any{"a": "x", "b": "", "c": nil, "d": 3})
	if len(got) != 2 || got["a"] != "x" || got["d"] != "3" {
		t.Errorf("stringArgs() = %v, want only the non-empty values as strings", got)
	}
}

func TestRenderResourceResultVariants(t *testing.T) {
	res := mcpclient.Resource{URI: "mem://x", Name: "x"}
	tests := []struct {
		name string
		msg  resourceResultMsg
		want string
	}{
		{"error", resourceResultMsg{res: res, err: errFake("nope")}, "read failed: nope"},
		{"empty", resourceResultMsg{res: res}, "no contents"},
		{"text", resourceResultMsg{res: res, contents: []mcpclient.ResourceContents{{URI: "mem://x", Text: "body"}}}, "body"},
		{"binary", resourceResultMsg{res: res, contents: []mcpclient.ResourceContents{{URI: "mem://x", MimeType: "image/png", Blob: "AAAA"}}}, "binary content, image/png"},
		{"blank", resourceResultMsg{res: res, contents: []mcpclient.ResourceContents{{URI: "mem://x"}}}, "(empty)"},
		{"truncated", resourceResultMsg{res: res, contents: []mcpclient.ResourceContents{{Text: strings.Repeat("x", maxShownBytes+10)}}}, "(truncated)"},
	}
	for _, tt := range tests {
		if got := renderResourceResult(tt.msg); !strings.Contains(got, tt.want) {
			t.Errorf("%s: output %q should contain %q", tt.name, got, tt.want)
		}
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestRenderPromptResultShowsArgsAndError(t *testing.T) {
	p := mcpclient.Prompt{Name: "greet"}
	ok := renderPromptResult(promptResultMsg{prompt: p, args: map[string]string{"who": "Ada", "tone": "warm"}, result: &mcpclient.GetPromptResult{
		Description: "A greeting",
		Messages:    []mcpclient.PromptMessage{{Role: "assistant", Content: mcpclient.ContentBlock{Type: "text", Text: "Hi Ada"}}},
	}})
	for _, want := range []string{"greet", "tone=warm, who=Ada", "A greeting", "assistant", "Hi Ada"} {
		if !strings.Contains(ok, want) {
			t.Errorf("rendered prompt should contain %q:\n%s", want, ok)
		}
	}
	if bad := renderPromptResult(promptResultMsg{prompt: p, err: errFake("bad")}); !strings.Contains(bad, "prompts/get failed: bad") {
		t.Errorf("error output = %q", bad)
	}
}

func TestItemAdapters(t *testing.T) {
	r := resourceItem{res: mcpclient.Resource{URI: "u", Name: ""}}
	if r.Title() != "u" || r.Description() != "u" {
		t.Errorf("a nameless resource falls back to its URI: %q / %q", r.Title(), r.Description())
	}
	r = resourceItem{res: mcpclient.Resource{URI: "u", Name: "n", Description: "d"}}
	if r.Title() != "n" || r.Description() != "d" || !strings.Contains(r.FilterValue(), "u") {
		t.Errorf("resource item = %q / %q / %q", r.Title(), r.Description(), r.FilterValue())
	}
	p := promptItem{prompt: mcpclient.Prompt{Name: "p"}}
	if p.Description() != "(no description)" || p.FilterValue() != "p" {
		t.Errorf("prompt item = %q / %q", p.Description(), p.FilterValue())
	}
	for s, want := range map[section]string{sectionTools: "Tools", sectionResources: "Resources", sectionPrompts: "Prompts"} {
		if s.label() != want {
			t.Errorf("section %d label = %q, want %q", s, s.label(), want)
		}
	}
}
