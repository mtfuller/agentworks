package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// asyncCall runs fn in a goroutine and returns a func that waits for it.
func asyncCall(t *testing.T, fn func()) func() {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	return func() {
		t.Helper()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("call did not return in time")
		}
	}
}

func TestListResourcesFollowsPagination(t *testing.T) {
	c, fs := newFakeServer(t)
	var got []Resource
	var err error
	wait := asyncCall(t, func() { got, err = c.ListResources(context.Background()) })

	req := fs.nextRequest()
	if req.Method != "resources/list" {
		t.Fatalf("method = %q, want resources/list", req.Method)
	}
	fs.respond(req.ID, map[string]any{"resources": []Resource{{URI: "a://1", Name: "one"}}, "nextCursor": "p2"})
	req = fs.nextRequest()
	if params, _ := json.Marshal(req.Params); !strings.Contains(string(params), "p2") {
		t.Errorf("second request params = %s, want the cursor", params)
	}
	fs.respond(req.ID, map[string]any{"resources": []Resource{{URI: "a://2", Name: "two", MimeType: "text/plain"}}})
	wait()

	if err != nil || len(got) != 2 || got[0].Name != "one" || got[1].MimeType != "text/plain" {
		t.Errorf("ListResources() = %+v, %v", got, err)
	}
}

func TestReadResource(t *testing.T) {
	c, fs := newFakeServer(t)
	var got []ResourceContents
	var err error
	wait := asyncCall(t, func() { got, err = c.ReadResource(context.Background(), "a://1") })

	req := fs.nextRequest()
	if req.Method != "resources/read" {
		t.Fatalf("method = %q", req.Method)
	}
	if params, _ := json.Marshal(req.Params); !strings.Contains(string(params), "a://1") {
		t.Errorf("params = %s, want the uri", params)
	}
	fs.respond(req.ID, map[string]any{"contents": []ResourceContents{{URI: "a://1", Text: "body"}}})
	wait()
	if err != nil || len(got) != 1 || got[0].Text != "body" {
		t.Errorf("ReadResource() = %+v, %v", got, err)
	}
}

func TestReadResourceRPCError(t *testing.T) {
	c, fs := newFakeServer(t)
	var err error
	wait := asyncCall(t, func() { _, err = c.ReadResource(context.Background(), "nope://x") })
	req := fs.nextRequest()
	fs.respondError(req.ID, -32002, "Resource not found")
	wait()

	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32002 || !strings.Contains(err.Error(), "Resource not found") {
		t.Errorf("ReadResource() error = %v, want the server's RPCError", err)
	}
}

func TestListPromptsAndGetPrompt(t *testing.T) {
	c, fs := newFakeServer(t)

	var prompts []Prompt
	var err error
	wait := asyncCall(t, func() { prompts, err = c.ListPrompts(context.Background()) })
	req := fs.nextRequest()
	if req.Method != "prompts/list" {
		t.Fatalf("method = %q", req.Method)
	}
	fs.respond(req.ID, map[string]any{"prompts": []Prompt{{Name: "greet", Arguments: []PromptArgument{{Name: "who", Required: true}}}}})
	wait()
	if err != nil || len(prompts) != 1 || !prompts[0].Arguments[0].Required {
		t.Fatalf("ListPrompts() = %+v, %v", prompts, err)
	}

	var result *GetPromptResult
	wait = asyncCall(t, func() { result, err = c.GetPrompt(context.Background(), "greet", map[string]string{"who": "Ada"}) })
	req = fs.nextRequest()
	if params, _ := json.Marshal(req.Params); !strings.Contains(string(params), `"who":"Ada"`) {
		t.Errorf("prompts/get params = %s, want the arguments", params)
	}
	fs.respond(req.ID, map[string]any{"description": "d", "messages": []map[string]any{{"role": "user", "content": map[string]any{"type": "text", "text": "Hi Ada"}}}})
	wait()
	if err != nil || result == nil || len(result.Messages) != 1 || result.Messages[0].Content.Text != "Hi Ada" {
		t.Errorf("GetPrompt() = %+v, %v", result, err)
	}

	// No arguments: the params carry only the name.
	wait = asyncCall(t, func() { _, err = c.GetPrompt(context.Background(), "plain", nil) })
	req = fs.nextRequest()
	if params, _ := json.Marshal(req.Params); strings.Contains(string(params), "arguments") {
		t.Errorf("params = %s, want no arguments key when there are none", params)
	}
	fs.respond(req.ID, map[string]any{"messages": []any{}})
	wait()
}

func TestPaginateReportsABadPage(t *testing.T) {
	c, fs := newFakeServer(t)
	var err error
	wait := asyncCall(t, func() { _, err = c.ListResources(context.Background()) })
	req := fs.nextRequest()
	fs.respond(req.ID, map[string]any{"resources": "not-a-list"})
	wait()
	if err == nil || !strings.Contains(err.Error(), "decoding resources/list") {
		t.Errorf("ListResources() error = %v, want a decode error", err)
	}
}

func TestHasCapability(t *testing.T) {
	r := InitializeResult{Capabilities: json.RawMessage(`{"tools":{},"resources":{"subscribe":true}}`)}
	if !r.HasCapability("tools") || !r.HasCapability("resources") || r.HasCapability("prompts") {
		t.Errorf("HasCapability wrong for %s", r.Capabilities)
	}
	if (InitializeResult{}).HasCapability("tools") {
		t.Error("no capabilities advertised means none")
	}
	if (InitializeResult{Capabilities: json.RawMessage(`[`)}).HasCapability("tools") {
		t.Error("malformed capabilities means none")
	}
}

func TestRPCErrorText(t *testing.T) {
	if got := (&RPCError{Code: -32601, Message: "no such method"}).Error(); !strings.Contains(got, "no such method") || !strings.Contains(got, "-32601") {
		t.Errorf("Error() = %q", got)
	}
}

// ---- Target / Open / Probe ------------------------------------------------

func TestOpenAndProbeOverEveryTransport(t *testing.T) {
	http := newFakeStreamable(t)
	sse := newFakeSSE(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := Probe(ctx, Target{Transport: TransportHTTP, URL: http.URL, Headers: map[string]string{"Authorization": "Bearer sekrit"}}, nil)
	if err != nil {
		t.Fatalf("Probe(http) error = %v", err)
	}
	if res.Info.ServerInfo.Name != "fake" || len(res.Tools) != 1 {
		t.Errorf("Probe(http) = %+v", res)
	}

	if len(res.Resources) != 1 || res.Resources[0].Name != "a" {
		t.Errorf("Probe(http) should list the resources the server advertised: %+v", res.Resources)
	}

	res, err = Probe(ctx, Target{Transport: TransportSSE, URL: sse.URL + "/sse"}, nil)
	if err != nil {
		t.Fatalf("Probe(sse) error = %v", err)
	}
	if res.Info.ServerInfo.Name != "sse-fake" || len(res.Tools) != 1 || res.Tools[0].Name != "ping" {
		t.Errorf("Probe(sse) = %+v", res)
	}
}

func TestOpenValidatesTheTarget(t *testing.T) {
	ctx := context.Background()
	for name, target := range map[string]Target{
		"http without a url":   {Transport: TransportHTTP},
		"sse without a url":    {Transport: TransportSSE},
		"an unknown transport": {Transport: "carrier-pigeon"},
	} {
		if conn, err := Open(ctx, target, nil, nil); err == nil {
			conn.Close()
			t.Errorf("%s: Open() expected an error", name)
		}
	}
	if !(Target{Transport: TransportHTTP}).IsRemote() || !(Target{Transport: TransportSSE}).IsRemote() || (Target{}).IsRemote() || (Target{Transport: TransportStdio}).IsRemote() {
		t.Error("IsRemote() wrong")
	}
}

func TestProbeReportsAFailedHandshake(t *testing.T) {
	http := newFakeStreamable(t)
	_, err := Probe(context.Background(), Target{Transport: TransportHTTP, URL: http.URL, Headers: map[string]string{"Authorization": "Bearer wrong"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Errorf("Probe() error = %v, want it to name the initialize step", err)
	}
}

// ---- a real local process -------------------------------------------------

// A shell one-liner is a complete stdio MCP server: it reads a line, and for
// each request id answers with a canned result. It exercises StartProcess, the
// stderr pipe, and Process.Close against a real child.
const shellServer = `
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"sh-server","version":"1"},"capabilities":{"tools":{}}}}\n' "$id"
      echo "hello from stderr" >&2 ;;
    *'"method":"tools/list"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"one"}]}}\n' "$id" ;;
  esac
done
`

func TestOpenAStdioServerAndCaptureItsStderr(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var stderr []string
	res, err := Probe(ctx, Target{Transport: TransportStdio, Command: shellServer}, func(line string) { stderr = append(stderr, line) })
	if err != nil {
		t.Fatalf("Probe(stdio) error = %v", err)
	}
	if res.Info.ServerInfo.Name != "sh-server" || len(res.Tools) != 1 || res.Tools[0].Name != "one" {
		t.Errorf("Probe(stdio) = %+v", res)
	}
	// Stderr is delivered asynchronously; give the pipe a moment after Close.
	deadline := time.Now().Add(2 * time.Second)
	for len(stderr) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(stderr) == 0 || stderr[0] != "hello from stderr" {
		t.Errorf("stderr = %v, want the server's stderr line forwarded", stderr)
	}
}

func TestStartProcessThatExitsImmediately(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Probe(ctx, Target{Transport: TransportStdio, Command: "exit 3"}, nil)
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Errorf("Probe() of a server that exits error = %v, want the handshake to fail", err)
	}
}

func TestProcessCloseKillsAServerThatIgnoresStdinEOF(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	orig := killGrace
	killGrace = 200 * time.Millisecond
	t.Cleanup(func() { killGrace = orig })

	// `sleep` never reads stdin, so closing it doesn't stop it.
	p, err := StartProcess("exec sleep 30", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = p.Close()
	if err == nil || !strings.Contains(err.Error(), "killed") {
		t.Errorf("Close() error = %v, want it to report the server was killed", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Close() should not wait out the server")
	}
}
